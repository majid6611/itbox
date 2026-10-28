package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"it-platform/chat/internal/directory"
	"it-platform/chat/internal/hub"
)

// A browser sends at most one typing notice every few seconds; the server
// enforces a looser floor so a script can't use the socket to spam people.
const typingMinGap = 1500 * time.Millisecond

// handleWebSocket sits outside huma (see router.go) since it needs the raw
// http.ResponseWriter/Request to hand to the websocket library's upgrade
// call. Auth still works the same way as every other endpoint — the
// browser sends the itp_employee_session cookie on the upgrade request
// same as any other request to this origin, so it's read directly here
// rather than through huma's cookie-binding input structs.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(employeeSessionCookieName)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	username, err := s.requireEmployeeAuth(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	token := cookie.Value
	s.Hub.Serve(w, r, username, func(ctx context.Context) bool {
		_, err := s.requireEmployeeAuth(ctx, token)
		return err == nil
	}, func(ctx context.Context, data []byte) {
		s.handleTyping(ctx, username, data)
	})
}

type typingIn struct {
	Type              string `json:"type"`
	GroupName         string `json:"group_name"`
	RecipientUsername string `json:"recipient_username"`
	CustomGroupID     int64  `json:"custom_group_id"`
}

// handleTyping relays "username is typing" to the others in a thread. The
// sender has to be allowed in the thread (same membership checks as
// sending), so a typing notice can't be used to probe or ping a thread
// they're not part of. Anything malformed or too frequent is ignored.
func (s *Server) handleTyping(ctx context.Context, username string, data []byte) {
	var in typingIn
	if json.Unmarshal(data, &in) != nil || in.Type != "typing" || targetCount(in.GroupName, in.RecipientUsername, in.CustomGroupID) != 1 {
		return
	}
	if !s.typingAllowed(username, time.Now()) {
		return
	}
	var to []string
	switch {
	case in.GroupName != "":
		if s.requireChannelMember(ctx, username, in.GroupName) != nil {
			return
		}
		members, err := directory.MembersOf(ctx, s.DB, in.GroupName)
		if err != nil {
			return
		}
		to = members
	case in.CustomGroupID != 0:
		if ok, err := s.isGroupMember(ctx, in.CustomGroupID, username); err != nil || !ok {
			return
		}
		members, err := s.groupMembers(ctx, in.CustomGroupID)
		if err != nil {
			return
		}
		to = members
	default:
		to = []string{in.RecipientUsername}
	}
	others := make([]string, 0, len(to))
	for _, u := range to {
		if u != username {
			others = append(others, u)
		}
	}
	s.Hub.SendTo(others, hub.Event{Type: "typing", Typing: &hub.Typing{
		Username: username, GroupName: in.GroupName, RecipientUsername: in.RecipientUsername, CustomGroupID: in.CustomGroupID,
	}})
}

func (s *Server) typingAllowed(username string, now time.Time) bool {
	s.typingMu.Lock()
	defer s.typingMu.Unlock()
	if s.typingAt == nil {
		s.typingAt = make(map[string]time.Time)
	}
	if now.Sub(s.typingAt[username]) < typingMinGap {
		return false
	}
	s.typingAt[username] = now
	return true
}
