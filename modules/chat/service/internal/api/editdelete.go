package api

import (
	"context"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type EditMessageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	ID           int64  `path:"id"`
	Body         struct {
		Content string `json:"content"`
	}
}

type EditMessageOutput struct {
	Body MessageOut
}

type DeleteMessageInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	ID           int64  `path:"id"`
}

type DeleteMessageOutput struct {
	Body MessageOut
}

func registerEditDelete(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "edit-chat-message",
		Method:      "PATCH",
		Path:        "/api/portal/chat/messages/{id}",
		Summary:     "Edit a message you sent — marks it edited, doesn't hide the change",
	}, func(ctx context.Context, in *EditMessageInput) (*EditMessageOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		content := strings.TrimSpace(in.Body.Content)
		if content == "" {
			return nil, huma.Error400BadRequest("message can't be empty")
		}

		msg, err := s.loadMessageForMutation(ctx, in.ID, username)
		if err != nil {
			return nil, err
		}
		if msg.DeletedAt != "" {
			return nil, huma.Error400BadRequest("can't edit a deleted message")
		}

		var editedAt time.Time
		err = s.DB.QueryRow(ctx, `
			UPDATE chat_messages SET content = $1, edited_at = now() WHERE id = $2 RETURNING edited_at
		`, content, in.ID).Scan(&editedAt)
		if err != nil {
			return nil, internalError("save edit", err)
		}
		msg.Content = content
		msg.EditedAt = editedAt.Format(time.RFC3339)

		if err := s.pushUpdate(ctx, msg); err != nil {
			return nil, internalError("deliver edit", err)
		}
		return &EditMessageOutput{Body: *msg}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-chat-message",
		Method:      "DELETE",
		Path:        "/api/portal/chat/messages/{id}",
		Summary:     "Delete a message you sent — soft delete, leaves a tombstone in its place",
	}, func(ctx context.Context, in *DeleteMessageInput) (*DeleteMessageOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}

		msg, err := s.loadMessageForMutation(ctx, in.ID, username)
		if err != nil {
			return nil, err
		}
		if msg.DeletedAt != "" {
			return &DeleteMessageOutput{Body: *msg}, nil
		}

		if msg.Attachment != nil {
			if err := s.deleteAttachment(ctx, msg.Attachment.ID); err != nil {
				return nil, internalError("remove attachment", err)
			}
			msg.Attachment = nil
		}

		var deletedAt time.Time
		err = s.DB.QueryRow(ctx, `
			UPDATE chat_messages SET content = '', deleted_at = now() WHERE id = $1 RETURNING deleted_at
		`, in.ID).Scan(&deletedAt)
		if err != nil {
			return nil, internalError("delete message", err)
		}
		msg.Content = ""
		msg.DeletedAt = deletedAt.Format(time.RFC3339)

		if err := s.pushUpdate(ctx, msg); err != nil {
			return nil, internalError("deliver delete", err)
		}
		return &DeleteMessageOutput{Body: *msg}, nil
	})
}

// loadMessageForMutation fetches a message by id and checks the caller is
// its sender — edit and delete are both sender-only, no admin override,
// same as every mainstream chat app.
func (s *Server) loadMessageForMutation(ctx context.Context, id int64, username string) (*MessageOut, error) {
	var m MessageOut
	var createdAt time.Time
	var editedAt, deletedAt *time.Time
	var groupName, recipient *string
	var customGroupID *int64
	err := s.DB.QueryRow(ctx, `
		SELECT id, sender_username, group_name, recipient_username, custom_group_id, content, created_at, edited_at, deleted_at
		FROM chat_messages WHERE id = $1
	`, id).Scan(&m.ID, &m.SenderUsername, &groupName, &recipient, &customGroupID, &m.Content, &createdAt, &editedAt, &deletedAt)
	if err != nil {
		return nil, huma.Error404NotFound("no such message")
	}
	if m.SenderUsername != username {
		return nil, huma.Error403Forbidden("you can only edit or delete your own messages")
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

	attachments, err := s.attachmentsFor(ctx, []int64{m.ID})
	if err != nil {
		return nil, internalError("load attachment", err)
	}
	if a, ok := attachments[m.ID]; ok {
		m.Attachment = &a
	}
	return &m, nil
}

// deleteAttachment removes an attachment's file from S3 and its DB row.
// Called when a message carrying an attachment is deleted — a tombstone
// shouldn't still serve the file it used to carry.
func (s *Server) deleteAttachment(ctx context.Context, attachmentID int) error {
	var key string
	err := s.DB.QueryRow(ctx, `SELECT s3_key FROM chat_attachments WHERE id = $1`, attachmentID).Scan(&key)
	if err != nil {
		return err
	}
	s3, available, err := s.chatS3Client(ctx)
	if err != nil {
		return err
	}
	if available {
		if err := s3.Delete(ctx, key); err != nil {
			return err
		}
	}
	_, err = s.DB.Exec(ctx, `DELETE FROM chat_attachments WHERE id = $1`, attachmentID)
	return err
}
