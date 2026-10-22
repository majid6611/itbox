// Package ami is a deliberately tiny Asterisk Manager Interface client —
// just enough to reload config and ask who's online. One short-lived TCP
// connection per call (login, action, logoff) rather than a long-lived
// session: there's nothing to keep in sync between calls, and it means an
// Asterisk restart never leaves a stale connection behind.
package ami

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"time"
)

type Client struct {
	Addr     string
	Username string
	Secret   string
}

// Message is one AMI response or event: its "Key: Value" lines.
type Message map[string]string

type session struct {
	conn net.Conn
	r    *textproto.Reader
}

func (c *Client) open(ctx context.Context) (*session, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return nil, fmt.Errorf("dial ami: %w", err)
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	s := &session{conn: conn, r: textproto.NewReader(bufio.NewReader(conn))}
	// Banner line: "Asterisk Call Manager/x.y.z"
	if _, err := s.r.ReadLine(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("read ami banner: %w", err)
	}
	resp, err := s.send(Message{"Action": "Login", "Username": c.Username, "Secret": c.Secret})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp["Response"] != "Success" {
		conn.Close()
		return nil, fmt.Errorf("ami login failed: %s", resp["Message"])
	}
	return s, nil
}

func (s *session) close() {
	s.write(Message{"Action": "Logoff"})
	s.conn.Close()
}

func (s *session) write(m Message) error {
	var b strings.Builder
	// Action first — AMI doesn't strictly require it, but it's how every
	// example and every Asterisk version expects to see it.
	fmt.Fprintf(&b, "Action: %s\r\n", m["Action"])
	for k, v := range m {
		if k != "Action" {
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	_, err := s.conn.Write([]byte(b.String()))
	return err
}

func (s *session) read() (Message, error) {
	m := Message{}
	for {
		line, err := s.r.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("read ami: %w", err)
		}
		if line == "" {
			if len(m) == 0 {
				continue
			}
			return m, nil
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
}

func (s *session) send(m Message) (Message, error) {
	if err := s.write(m); err != nil {
		return nil, fmt.Errorf("write ami: %w", err)
	}
	for {
		resp, err := s.read()
		if err != nil {
			return nil, err
		}
		if _, ok := resp["Response"]; ok {
			return resp, nil
		}
		// An unsolicited event between our action and its response —
		// ignore it, we only care about the response.
	}
}

// Command runs CLI commands (e.g. "dialplan reload") in one session.
func (c *Client) Command(ctx context.Context, commands ...string) error {
	s, err := c.open(ctx)
	if err != nil {
		return err
	}
	defer s.close()
	for _, cmd := range commands {
		resp, err := s.send(Message{"Action": "Command", "Command": cmd})
		if err != nil {
			return err
		}
		if resp["Response"] == "Error" {
			return fmt.Errorf("ami command %q: %s", cmd, resp["Message"])
		}
	}
	return nil
}

// List runs a list-style action (PJSIPShowEndpoints, ...) and returns
// every event named itemEvent until the list's completion event.
func (c *Client) List(ctx context.Context, action, itemEvent string) ([]Message, error) {
	s, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()
	resp, err := s.send(Message{"Action": action})
	if err != nil {
		return nil, err
	}
	if resp["Response"] != "Success" {
		// Asterisk answers an empty list with an Error ("No endpoints
		// found") rather than an empty success — not a failure here.
		return nil, nil
	}
	var items []Message
	for {
		ev, err := s.read()
		if err != nil {
			return nil, err
		}
		if ev["EventList"] == "Complete" {
			return items, nil
		}
		if ev["Event"] == itemEvent {
			items = append(items, ev)
		}
	}
}

// Getvar reads a global variable or dialplan function (e.g.
// "ENV(SERVER_IP)") — "" if it's unset.
func (c *Client) Getvar(ctx context.Context, variable string) (string, error) {
	s, err := c.open(ctx)
	if err != nil {
		return "", err
	}
	defer s.close()
	resp, err := s.send(Message{"Action": "Getvar", "Variable": variable})
	if err != nil {
		return "", err
	}
	if resp["Response"] != "Success" {
		return "", fmt.Errorf("ami getvar %q: %s", variable, resp["Message"])
	}
	return resp["Value"], nil
}
