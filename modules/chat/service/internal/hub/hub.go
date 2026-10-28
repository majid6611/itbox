// Package hub manages live WebSocket connections: who's connected, pushing
// events to specific users or everyone, and presence (who's online).
// Deliberately knows nothing about groups/DMs/messages — that's the API
// layer's job, which already knows who should receive a given message and
// just calls SendTo with the right usernames. Presence is pure in-memory
// state, not a database table (see the package doc on why: it's live
// connection state, not durable data) — correct at today's single-instance
// scale; moving it to a shared store later (if the platform ever runs
// multiple backend instances) would replace this package without touching
// anything that calls it.
package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// A laptop sleeping or WiFi dying doesn't send a clean close frame, so
// presence can't rely on disconnect events alone — the server pings every
// pingInterval and gives up on the connection if a pong doesn't arrive
// within pingTimeout, which is what actually keeps "online" accurate.
const (
	pingInterval = 30 * time.Second
	pingTimeout  = 10 * time.Second
)

type Event struct {
	// "message" is a newly-sent message; "message_updated" is an edit or a
	// delete on an existing one (Message.DeletedAt set for a delete) — the
	// client finds the message by ID in whichever thread it belongs to and
	// replaces it in place, same shape either way.
	// "typing" says someone is typing in a thread; "group_changed" tells
	// members of a private group (and anyone just removed from it) to refetch
	// their group list.
	Type     string       `json:"type"` // "message" | "message_updated" | "presence" | "group_invite" | "group_changed" | "typing"
	Typing   *Typing      `json:"typing,omitempty"`
	Message  *Message     `json:"message,omitempty"`
	Presence *Presence    `json:"presence,omitempty"`
	Group    *GroupInvite `json:"group,omitempty"`
}

// GroupInvite tells a newly-added member's own open tabs to refetch their
// group list live — being added to a group is otherwise silent until they
// reload, the same class of "went stale without a refresh" gap as an
// unnoticed new message.
type GroupInvite struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Typing names the thread the same way a Message does (exactly one of the
// three target fields is set).
type Typing struct {
	Username          string `json:"username"`
	GroupName         string `json:"group_name,omitempty"`
	RecipientUsername string `json:"recipient_username,omitempty"`
	CustomGroupID     int64  `json:"custom_group_id,omitempty"`
}

type Reply struct {
	ID      int64  `json:"id"`
	Sender  string `json:"sender_username"`
	Content string `json:"content"`
	Deleted bool   `json:"deleted,omitempty"`
}

type Message struct {
	ID                int64       `json:"id"`
	SenderUsername    string      `json:"sender_username"`
	GroupName         string      `json:"group_name,omitempty"`
	RecipientUsername string      `json:"recipient_username,omitempty"`
	CustomGroupID     int64       `json:"custom_group_id,omitempty"`
	Content           string      `json:"content"`
	CreatedAt         string      `json:"created_at"`
	EditedAt          string      `json:"edited_at,omitempty"`
	DeletedAt         string      `json:"deleted_at,omitempty"`
	Attachment        *Attachment `json:"attachment,omitempty"`
	ReplyTo           *Reply      `json:"reply_to,omitempty"`
}

type Attachment struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size_bytes"`
}

type Presence struct {
	Username string `json:"username"`
	Online   bool   `json:"online"`
}

type conn struct {
	username string
	send     chan []byte
}

type Hub struct {
	mu    sync.Mutex
	conns map[string]map[*conn]bool // username -> that user's connections (multiple tabs/devices)
}

func New() *Hub {
	return &Hub{conns: make(map[string]map[*conn]bool)}
}

// Online lists every currently-connected username.
func (h *Hub) Online() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for u := range h.conns {
		out = append(out, u)
	}
	return out
}

func (h *Hub) IsOnline(username string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns[username]) > 0
}

// SendTo delivers an event to every connection each of the given usernames
// currently has open. Silently does nothing for anyone offline — they'll
// pick the message up via the ordinary history backfill on their next
// connect, per the durability design (Postgres is the source of truth,
// this is just a live courtesy push).
func (h *Hub) SendTo(usernames []string, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, u := range usernames {
		for c := range h.conns[u] {
			select {
			case c.send <- data:
			default: // slow consumer — drop rather than stall the whole hub
			}
		}
	}
}

// Broadcast delivers an event to every connected user — used for presence
// changes, which everyone in a small company plausibly cares about.
func (h *Hub) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, set := range h.conns {
		for c := range set {
			select {
			case c.send <- data:
			default:
			}
		}
	}
}

func (h *Hub) register(username string) *conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := &conn{username: username, send: make(chan []byte, 32)}
	if h.conns[username] == nil {
		h.conns[username] = make(map[*conn]bool)
	}
	h.conns[username][c] = true
	return c
}

// unregister reports whether this was the user's last open connection —
// only then should a presence-offline event actually go out.
func (h *Hub) unregister(c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns[c.username], c)
	last := len(h.conns[c.username]) == 0
	if last {
		delete(h.conns, c.username)
	}
	close(c.send)
	return last
}

// Serve upgrades the request to a WebSocket for username and runs the
// connection until it dies (client close, network failure, or a missed
// heartbeat). Blocks until then — call in its own goroutine per request.
// Serve runs one WebSocket connection. stillValid is asked on every ping
// tick whether the person behind it should still be connected (their
// session exists); once it says no — they were disabled or deleted, or their
// password was reset — the connection is closed, instead of carrying on
// receiving other people's messages for as long as the tab stays open.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, username string, stillValid func(context.Context) bool, onClient func(context.Context, []byte)) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	c := h.register(username)
	if h.isOnlyConn(username, c) {
		h.Broadcast(Event{Type: "presence", Presence: &Presence{Username: username, Online: true}})
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case data, ok := <-c.send:
				if !ok {
					return
				}
				// Checked per message too, not just on the ping: otherwise a
				// disabled person would keep receiving for up to a ping interval.
				if !stillValid(r.Context()) {
					ws.Close(websocket.StatusPolicyViolation, "session ended")
					return
				}
				wctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				err := ws.Write(wctx, websocket.MessageText, data)
				cancel()
				if err != nil {
					ws.Close(websocket.StatusInternalError, "write failed")
					return
				}
			case <-ticker.C:
				if !stillValid(r.Context()) {
					ws.Close(websocket.StatusPolicyViolation, "session ended")
					return
				}
				pctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
				err := ws.Ping(pctx)
				cancel()
				if err != nil {
					ws.Close(websocket.StatusGoingAway, "ping timeout")
					return
				}
			}
		}
	}()

	// Sending a message is a normal POST, not this socket. The only thing a
	// client says over it is a typing notice, handed to onClient; the rest
	// of the time this loop is just waiting for the connection to die.
	for {
		_, data, err := ws.Read(r.Context())
		if err != nil {
			break
		}
		onClient(r.Context(), data)
	}

	ws.Close(websocket.StatusNormalClosure, "")
	last := h.unregister(c)
	<-writerDone
	if last {
		h.Broadcast(Event{Type: "presence", Presence: &Presence{Username: username, Online: false}})
	}
}

func (h *Hub) isOnlyConn(username string, c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns[username]) == 1 && h.conns[username][c]
}
