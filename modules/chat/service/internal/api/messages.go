package api

import (
	"context"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"it-platform/chat/internal/hub"
)

type GetMessagesInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Group        string `query:"group"`
	With         string `query:"with"`
	// After is a message id cursor — 0 means "no cursor, give me the last
	// 50" (initial load); a real id means "everything since this"
	// (reconnect backfill). This, not the WebSocket, is the actual
	// delivery guarantee — see the package doc on hub for why.
	After int64 `query:"after"`
}

type GetMessagesOutput struct {
	Body struct {
		Messages []MessageOut `json:"messages"`
	}
}

type SendMessageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Body         struct {
		GroupName         string `json:"group_name,omitempty"`
		RecipientUsername string `json:"recipient_username,omitempty"`
		Content           string `json:"content"`
	}
}

type SendMessageOutput struct {
	Body MessageOut
}

func registerMessages(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-chat-messages",
		Method:      "GET",
		Path:        "/api/portal/chat/messages",
		Summary:     "Get channel or DM history — the last 50 messages, or everything after a given id",
	}, func(ctx context.Context, in *GetMessagesInput) (*GetMessagesOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		if (in.Group == "") == (in.With == "") {
			return nil, huma.Error400BadRequest("specify exactly one of group or with")
		}

		messages, err := s.fetchMessages(ctx, username, in.Group, in.With, in.After)
		if err != nil {
			return nil, huma.Error500InternalServerError("load messages", err)
		}
		out := &GetMessagesOutput{}
		out.Body.Messages = messages
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "send-chat-message",
		Method:      "POST",
		Path:        "/api/portal/chat/messages",
		Summary:     "Send a group or DM text message",
	}, func(ctx context.Context, in *SendMessageInput) (*SendMessageOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		content := strings.TrimSpace(in.Body.Content)
		if content == "" {
			return nil, huma.Error400BadRequest("message can't be empty")
		}
		if (in.Body.GroupName == "") == (in.Body.RecipientUsername == "") {
			return nil, huma.Error400BadRequest("specify exactly one of group_name or recipient_username")
		}

		msg, err := s.insertMessage(ctx, username, in.Body.GroupName, in.Body.RecipientUsername, content)
		if err != nil {
			return nil, huma.Error500InternalServerError("save message", err)
		}
		s.pushMessage(msg)

		out := &SendMessageOutput{Body: *msg}
		return out, nil
	})
}

// fetchMessages implements the two shapes described on GetMessagesInput.After
// for both a group channel and a DM, then attaches any file attachments in
// one follow-up query rather than one per message.
func (s *Server) fetchMessages(ctx context.Context, me, group, with string, after int64) ([]MessageOut, error) {
	var rows pgx.Rows
	var err error
	switch {
	case group != "" && after > 0:
		rows, err = s.DB.Query(ctx, `
			SELECT id, sender_username, group_name, recipient_username, content, created_at
			FROM chat_messages WHERE group_name = $1 AND id > $2 ORDER BY id ASC LIMIT 500`, group, after)
	case group != "":
		rows, err = s.DB.Query(ctx, `
			SELECT id, sender_username, group_name, recipient_username, content, created_at FROM (
				SELECT id, sender_username, group_name, recipient_username, content, created_at
				FROM chat_messages WHERE group_name = $1 ORDER BY id DESC LIMIT 50
			) recent ORDER BY id ASC`, group)
	case after > 0:
		rows, err = s.DB.Query(ctx, `
			SELECT id, sender_username, group_name, recipient_username, content, created_at
			FROM chat_messages
			WHERE ((sender_username = $1 AND recipient_username = $2) OR (sender_username = $2 AND recipient_username = $1)) AND id > $3
			ORDER BY id ASC LIMIT 500`, me, with, after)
	default:
		rows, err = s.DB.Query(ctx, `
			SELECT id, sender_username, group_name, recipient_username, content, created_at FROM (
				SELECT id, sender_username, group_name, recipient_username, content, created_at
				FROM chat_messages
				WHERE (sender_username = $1 AND recipient_username = $2) OR (sender_username = $2 AND recipient_username = $1)
				ORDER BY id DESC LIMIT 50
			) recent ORDER BY id ASC`, me, with)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []MessageOut
	ids := make([]int64, 0)
	for rows.Next() {
		var m MessageOut
		var createdAt time.Time
		var groupName, recipient *string
		if err := rows.Scan(&m.ID, &m.SenderUsername, &groupName, &recipient, &m.Content, &createdAt); err != nil {
			return nil, err
		}
		if groupName != nil {
			m.GroupName = *groupName
		}
		if recipient != nil {
			m.RecipientUsername = *recipient
		}
		m.CreatedAt = createdAt.Format(time.RFC3339)
		messages = append(messages, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	attachments, err := s.attachmentsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range messages {
		if a, ok := attachments[messages[i].ID]; ok {
			messages[i].Attachment = &a
		}
	}
	return messages, nil
}

func (s *Server) attachmentsFor(ctx context.Context, messageIDs []int64) (map[int64]AttachmentOut, error) {
	out := make(map[int64]AttachmentOut)
	if len(messageIDs) == 0 {
		return out, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT message_id, id, filename, size_bytes FROM chat_attachments WHERE message_id = ANY($1)`, messageIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var messageID int64
		var a AttachmentOut
		if err := rows.Scan(&messageID, &a.ID, &a.Filename, &a.Size); err != nil {
			return nil, err
		}
		out[messageID] = a
	}
	return out, rows.Err()
}

// insertMessage writes a message and returns it in the same shape the live
// push and the history endpoint both use.
func (s *Server) insertMessage(ctx context.Context, sender, group, recipient, content string) (*MessageOut, error) {
	var groupArg, recipientArg *string
	if group != "" {
		groupArg = &group
	}
	if recipient != "" {
		recipientArg = &recipient
	}
	m := &MessageOut{SenderUsername: sender, GroupName: group, RecipientUsername: recipient, Content: content}
	var createdAt time.Time
	err := s.DB.QueryRow(ctx, `
		INSERT INTO chat_messages (sender_username, group_name, recipient_username, content)
		VALUES ($1, $2, $3, $4) RETURNING id, created_at
	`, sender, groupArg, recipientArg, content).Scan(&m.ID, &createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = createdAt.Format(time.RFC3339)
	return m, nil
}

// pushMessage delivers a just-sent message live. Group messages go to
// everyone connected — channels are open to the whole company (chat has no
// per-page permission model like wiki's, that wasn't asked for), so
// anyone might have that channel open. DMs go only to the two
// participants, including the sender's own other tabs/devices for a
// consistent multi-device view.
func (s *Server) pushMessage(m *MessageOut) {
	hm := &hub.Message{
		ID: m.ID, SenderUsername: m.SenderUsername, GroupName: m.GroupName,
		RecipientUsername: m.RecipientUsername, Content: m.Content, CreatedAt: m.CreatedAt,
	}
	if m.Attachment != nil {
		hm.Attachment = &hub.Attachment{ID: m.Attachment.ID, Filename: m.Attachment.Filename, Size: m.Attachment.Size}
	}
	event := hub.Event{Type: "message", Message: hm}
	if m.GroupName != "" {
		s.Hub.Broadcast(event)
	} else {
		s.Hub.SendTo([]string{m.SenderUsername, m.RecipientUsername}, event)
	}
}
