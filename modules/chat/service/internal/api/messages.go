package api

import (
	"context"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"it-platform/chat/internal/directory"
	"it-platform/chat/internal/hub"
)

type GetMessagesInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Group        string `query:"group"`
	With         string `query:"with"`
	CustomGroup  int64  `query:"custom_group"`
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
		CustomGroupID     int64  `json:"custom_group_id,omitempty"`
		Content           string `json:"content"`
	}
}

type SendMessageOutput struct {
	Body MessageOut
}

// targetCount reports how many of the three mutually-exclusive message
// targets are set — every send/history endpoint needs exactly one.
func targetCount(group, with string, customGroup int64) int {
	n := 0
	if group != "" {
		n++
	}
	if with != "" {
		n++
	}
	if customGroup != 0 {
		n++
	}
	return n
}

func registerMessages(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-chat-messages",
		Method:      "GET",
		Path:        "/api/portal/chat/messages",
		Summary:     "Get channel, DM, or private-group history — the last 50 messages, or everything after a given id",
	}, func(ctx context.Context, in *GetMessagesInput) (*GetMessagesOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		if targetCount(in.Group, in.With, in.CustomGroup) != 1 {
			return nil, huma.Error400BadRequest("specify exactly one of group, with, or custom_group")
		}
		if in.Group != "" {
			if err := s.requireChannelMember(ctx, username, in.Group); err != nil {
				return nil, err
			}
		}
		if in.CustomGroup != 0 {
			isMember, err := s.isGroupMember(ctx, in.CustomGroup, username)
			if err != nil {
				return nil, huma.Error500InternalServerError("check membership", err)
			}
			if !isMember {
				return nil, huma.Error403Forbidden("you're not a member of this group")
			}
		}

		messages, err := s.fetchMessages(ctx, username, in.Group, in.With, in.CustomGroup, in.After)
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
		Summary:     "Send a text message to a channel, a DM, or a private group",
	}, func(ctx context.Context, in *SendMessageInput) (*SendMessageOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		content := strings.TrimSpace(in.Body.Content)
		if content == "" {
			return nil, huma.Error400BadRequest("message can't be empty")
		}
		if targetCount(in.Body.GroupName, in.Body.RecipientUsername, in.Body.CustomGroupID) != 1 {
			return nil, huma.Error400BadRequest("specify exactly one of group_name, recipient_username, or custom_group_id")
		}
		if in.Body.GroupName != "" {
			if err := s.requireChannelMember(ctx, username, in.Body.GroupName); err != nil {
				return nil, err
			}
		}
		if in.Body.CustomGroupID != 0 {
			isMember, err := s.isGroupMember(ctx, in.Body.CustomGroupID, username)
			if err != nil {
				return nil, huma.Error500InternalServerError("check membership", err)
			}
			if !isMember {
				return nil, huma.Error403Forbidden("you're not a member of this group")
			}
		}

		msg, err := s.insertMessage(ctx, username, in.Body.GroupName, in.Body.RecipientUsername, in.Body.CustomGroupID, content)
		if err != nil {
			return nil, huma.Error500InternalServerError("save message", err)
		}
		if err := s.pushMessage(ctx, msg); err != nil {
			return nil, huma.Error500InternalServerError("deliver message", err)
		}

		out := &SendMessageOutput{Body: *msg}
		return out, nil
	})
}

// fetchMessages implements the two shapes described on GetMessagesInput.After
// for a group channel, a DM, or a private group, then attaches any file
// attachments in one follow-up query rather than one per message. Caller
// must have already checked group membership for a private-group target —
// this assumes access is already allowed.
func (s *Server) fetchMessages(ctx context.Context, me, group, with string, customGroup int64, after int64) ([]MessageOut, error) {
	const cols = "id, sender_username, group_name, recipient_username, custom_group_id, content, created_at, edited_at, deleted_at"
	var rows pgx.Rows
	var err error
	switch {
	case group != "" && after > 0:
		rows, err = s.DB.Query(ctx, `SELECT `+cols+` FROM chat_messages WHERE group_name = $1 AND id > $2 ORDER BY id ASC LIMIT 500`, group, after)
	case group != "":
		rows, err = s.DB.Query(ctx, `SELECT `+cols+` FROM (
			SELECT `+cols+` FROM chat_messages WHERE group_name = $1 ORDER BY id DESC LIMIT 50
		) recent ORDER BY id ASC`, group)
	case customGroup != 0 && after > 0:
		rows, err = s.DB.Query(ctx, `SELECT `+cols+` FROM chat_messages WHERE custom_group_id = $1 AND id > $2 ORDER BY id ASC LIMIT 500`, customGroup, after)
	case customGroup != 0:
		rows, err = s.DB.Query(ctx, `SELECT `+cols+` FROM (
			SELECT `+cols+` FROM chat_messages WHERE custom_group_id = $1 ORDER BY id DESC LIMIT 50
		) recent ORDER BY id ASC`, customGroup)
	case after > 0:
		rows, err = s.DB.Query(ctx, `
			SELECT `+cols+` FROM chat_messages
			WHERE ((sender_username = $1 AND recipient_username = $2) OR (sender_username = $2 AND recipient_username = $1)) AND id > $3
			ORDER BY id ASC LIMIT 500`, me, with, after)
	default:
		rows, err = s.DB.Query(ctx, `SELECT `+cols+` FROM (
			SELECT `+cols+` FROM chat_messages
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
		var editedAt, deletedAt *time.Time
		var groupName, recipient *string
		var customGroupID *int64
		if err := rows.Scan(&m.ID, &m.SenderUsername, &groupName, &recipient, &customGroupID, &m.Content, &createdAt, &editedAt, &deletedAt); err != nil {
			return nil, err
		}
		if groupName != nil {
			m.GroupName = *groupName
		}
		if recipient != nil {
			m.RecipientUsername = *recipient
		}
		if customGroupID != nil {
			m.CustomGroupID = *customGroupID
		}
		m.CreatedAt = createdAt.Format(time.RFC3339)
		if editedAt != nil {
			m.EditedAt = editedAt.Format(time.RFC3339)
		}
		if deletedAt != nil {
			m.DeletedAt = deletedAt.Format(time.RFC3339)
		}
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
// push and the history endpoint both use. Caller must have already
// validated the target and, for a private group, membership.
func (s *Server) insertMessage(ctx context.Context, sender, group, recipient string, customGroupID int64, content string) (*MessageOut, error) {
	var groupArg, recipientArg *string
	var customGroupArg *int64
	if group != "" {
		groupArg = &group
	}
	if recipient != "" {
		recipientArg = &recipient
	}
	if customGroupID != 0 {
		customGroupArg = &customGroupID
	}
	m := &MessageOut{SenderUsername: sender, GroupName: group, RecipientUsername: recipient, CustomGroupID: customGroupID, Content: content}
	var createdAt time.Time
	err := s.DB.QueryRow(ctx, `
		INSERT INTO chat_messages (sender_username, group_name, recipient_username, custom_group_id, content)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at
	`, sender, groupArg, recipientArg, customGroupArg, content).Scan(&m.ID, &createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = createdAt.Format(time.RFC3339)
	return m, nil
}

// pushMessage delivers a just-sent message live. See pushEvent for the
// routing rules (open channel vs. DM vs. private-group membership).
func (s *Server) pushMessage(ctx context.Context, m *MessageOut) error {
	return s.pushEvent(ctx, m, "message")
}

// pushUpdate delivers an edit or a delete on an existing message the same
// way pushMessage delivers a new one — same target, same routing rules,
// just a different event type so the client knows to find-and-replace
// rather than append.
func (s *Server) pushUpdate(ctx context.Context, m *MessageOut) error {
	return s.pushEvent(ctx, m, "message_updated")
}

// pushEvent is the shared routing: an LDAP-group channel message goes only
// to that group's actual members (mirroring the read/write REST checks —
// live delivery shouldn't reach further than history does); DMs go only to
// the two participants (including the sender's own other tabs/devices, for
// a consistent multi-device view); private-group messages go only to that
// group's actual members — the same invisibility guarantee the group's
// existence gets extends to its live traffic, not just its history.
func (s *Server) pushEvent(ctx context.Context, m *MessageOut, eventType string) error {
	hm := &hub.Message{
		ID: m.ID, SenderUsername: m.SenderUsername, GroupName: m.GroupName,
		RecipientUsername: m.RecipientUsername, CustomGroupID: m.CustomGroupID,
		Content: m.Content, CreatedAt: m.CreatedAt, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt,
	}
	if m.Attachment != nil {
		hm.Attachment = &hub.Attachment{ID: m.Attachment.ID, Filename: m.Attachment.Filename, Size: m.Attachment.Size}
	}
	event := hub.Event{Type: eventType, Message: hm}
	switch {
	case m.GroupName != "":
		members, err := directory.MembersOf(ctx, s.DB, m.GroupName)
		if err != nil {
			return err
		}
		s.Hub.SendTo(members, event)
	case m.CustomGroupID != 0:
		members, err := s.groupMembers(ctx, m.CustomGroupID)
		if err != nil {
			return err
		}
		s.Hub.SendTo(members, event)
	default:
		s.Hub.SendTo([]string{m.SenderUsername, m.RecipientUsername}, event)
	}
	return nil
}
