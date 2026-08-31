// Package meshcentral wraps MeshCentral's control WebSocket protocol
// (wss://<host>/control.ashx, `x-meshauth` header auth, JSON action
// messages correlated by responseid) — no client library exists for this
// in Go, so this is a from-scratch client covering exactly the actions
// this platform needs: create the one device group, register/remove an
// Intel AMT device, list devices, and issue power commands. Verified
// against a real running MeshCentral container before being written, not
// guessed from the protocol's (undocumented) source alone — see the
// compute-mesh module's own notes.
//
// Deliberately never exposes MeshCentral's own UI or account model to
// anyone using this platform — one shared service account, provisioned
// once at module install, is the only thing that ever talks to it.
package meshcentral

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// AMT power actiontype values MeshCentral's protocol expects — the only
// three of many it supports that this platform surfaces (no KVM, no
// serial-over-LAN, no IDE-redirection; those are a much bigger protocol
// surface AMT also exposes and are out of scope here).
const (
	ActionAMTPowerOn    = 302
	ActionAMTPowerOff   = 308
	ActionAMTPowerCycle = 310
)

type Client struct {
	addr     string // internal Docker hostname, e.g. itp-compute-mesh-meshcentral-1
	username string
	password string
}

func NewClient(addr, username, password string) *Client {
	return &Client{addr: addr, username: username, password: password}
}

// call opens a fresh connection, sends one action, and returns the first
// message whose responseid matches — then closes. A connection per call
// rather than a persistent one: these are occasional, low-frequency admin
// actions (register a device, click power on), not a real-time stream, so
// there's no reconnect/multiplexing complexity worth taking on for it.
func (c *Client) call(ctx context.Context, action map[string]any) (map[string]any, error) {
	authHeader := base64.StdEncoding.EncodeToString([]byte(c.username)) + "," + base64.StdEncoding.EncodeToString([]byte(c.password))
	httpClient := &http.Client{Transport: &http.Transport{
		// This connection never leaves the Docker network — unlike
		// video-jitsi's browser-facing case, nothing here needs a
		// trusted cert, just an encrypted hop to a container we already
		// trust by address.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}}

	conn, _, err := websocket.Dial(ctx, "wss://"+c.addr+"/control.ashx", &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: http.Header{"x-meshauth": []string{authHeader}},
	})
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.CloseNow()

	const responseID = "req"
	action["responseid"] = responseID
	if err := wsjson.Write(ctx, conn, action); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	for {
		var msg map[string]any
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
		if msg["responseid"] == responseID {
			conn.Close(websocket.StatusNormalClosure, "")
			return msg, nil
		}
		// An unsolicited event (serverinfo/userinfo/account/mesh/node
		// change notifications MeshCentral pushes unprompted) — not what
		// we asked for, keep reading for the real response.
	}
}

func resultOK(msg map[string]any) error {
	if result, _ := msg["result"].(string); result != "ok" {
		return fmt.Errorf("meshcentral: %v", msg["result"])
	}
	return nil
}

// CreateMesh creates a device group scoped to Intel-AMT-only devices
// (meshtype 1 — confirmed live: meshtype 2, the default "agent" group
// type, rejects addamtdevice with "Intel AMT agentless mesh only
// allowed"). This platform creates exactly one, lazily, the first time
// it's needed — MeshCentral's own multi-group model has no equivalent in
// our single flat device list.
func (c *Client) CreateMesh(ctx context.Context, name string) (meshID string, err error) {
	msg, err := c.call(ctx, map[string]any{"action": "createmesh", "meshname": name, "meshtype": 1})
	if err != nil {
		return "", err
	}
	if err := resultOK(msg); err != nil {
		return "", err
	}
	meshID, _ = msg["meshid"].(string)
	return meshID, nil
}

// AddAMTDevice registers a device by its AMT host/IP and credentials —
// exactly "admin, password, IP" as the target's actual power-control
// path, nothing installed on the target itself.
func (c *Client) AddAMTDevice(ctx context.Context, meshID, name, host, amtUsername, amtPassword string) error {
	msg, err := c.call(ctx, map[string]any{
		"action": "addamtdevice", "meshid": meshID, "devicename": name,
		"hostname": host, "amtusername": amtUsername, "amtpassword": amtPassword, "amttls": 1,
	})
	if err != nil {
		return err
	}
	return resultOK(msg)
}

// Nodes returns every device name -> MeshCentral node id currently
// registered in the given mesh.
func (c *Client) Nodes(ctx context.Context, meshID string) (map[string]string, error) {
	msg, err := c.call(ctx, map[string]any{"action": "nodes", "meshid": meshID})
	if err != nil {
		return nil, err
	}
	nodesByMesh, _ := msg["nodes"].(map[string]any)
	list, _ := nodesByMesh[meshID].([]any)
	out := make(map[string]string, len(list))
	for _, raw := range list {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := node["name"].(string)
		id, _ := node["_id"].(string)
		if name != "" && id != "" {
			out[name] = id
		}
	}
	return out, nil
}

// RemoveDevice deletes a device from MeshCentral entirely.
func (c *Client) RemoveDevice(ctx context.Context, nodeID string) error {
	msg, err := c.call(ctx, map[string]any{"action": "removedevices", "nodeids": []string{nodeID}})
	if err != nil {
		return err
	}
	return resultOK(msg)
}

// PowerAction issues an Intel AMT power command — actionType is one of
// the ActionAMTPower* constants above. Fire-and-forget: MeshCentral
// acknowledges the command was accepted and queued, not that the physical
// machine actually responded (confirmed live — poweraction against an
// address with no real device on the other end still returns "ok").
func (c *Client) PowerAction(ctx context.Context, nodeIDs []string, actionType int) error {
	msg, err := c.call(ctx, map[string]any{"action": "poweraction", "nodeids": nodeIDs, "actiontype": actionType})
	if err != nil {
		return err
	}
	return resultOK(msg)
}
