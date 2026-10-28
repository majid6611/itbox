package api

import (
	"context"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type UnreadInput struct {
	SessionToken string `cookie:"itp_employee_session"`
}

type UnreadOutput struct {
	Body struct {
		// Counts maps a thread key ("group:<name>", "custom:<id>",
		// "dm:<username>") to its number of unread messages from others.
		Counts map[string]int `json:"counts"`
	}
}

type MarkReadInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Body         struct {
		Key       string `json:"key"`
		MessageID int64  `json:"message_id"`
	}
}

// A thread with no read-state row counts as read up to the baseline set by
// the migration (see 0004_read_state.sql), so an existing install's old
// history doesn't all show up as unread.
const baselineSQL = `COALESCE((SELECT message_id FROM chat_read_baseline WHERE id = 1), 0)`

func registerReadState(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-chat-unread",
		Method:      "GET",
		Path:        "/api/portal/chat/unread",
		Summary:     "Unread message counts per thread, for the caller",
	}, func(ctx context.Context, in *UnreadInput) (*UnreadOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		counts := map[string]int{}

		if err := s.addCounts(ctx, counts, `
			SELECT 'dm:' || m.sender_username, count(*)
			FROM chat_messages m
			LEFT JOIN chat_read_state r ON r.username = $1 AND r.target_key = 'dm:' || m.sender_username
			WHERE m.recipient_username = $1 AND m.deleted_at IS NULL
			  AND m.id > COALESCE(r.last_read_id, `+baselineSQL+`)
			GROUP BY 1`, username); err != nil {
			return nil, internalError("count unread DMs", err)
		}

		if err := s.addCounts(ctx, counts, `
			SELECT 'custom:' || m.custom_group_id, count(*)
			FROM chat_messages m
			JOIN chat_group_members gm ON gm.group_id = m.custom_group_id AND gm.username = $1
			LEFT JOIN chat_read_state r ON r.username = $1 AND r.target_key = 'custom:' || m.custom_group_id
			WHERE m.sender_username <> $1 AND m.deleted_at IS NULL
			  AND m.id > COALESCE(r.last_read_id, `+baselineSQL+`)
			GROUP BY 1`, username); err != nil {
			return nil, internalError("count unread group messages", err)
		}

		group, err := s.employeeGroup(ctx, username)
		if err != nil {
			return nil, internalError("look up group", err)
		}
		if group != "" {
			if err := s.addCounts(ctx, counts, `
				SELECT 'group:' || m.group_name, count(*)
				FROM chat_messages m
				LEFT JOIN chat_read_state r ON r.username = $1 AND r.target_key = 'group:' || m.group_name
				WHERE m.group_name = $2 AND m.sender_username <> $1 AND m.deleted_at IS NULL
				  AND m.id > COALESCE(r.last_read_id, `+baselineSQL+`)
				GROUP BY 1`, username, group); err != nil {
				return nil, internalError("count unread channel messages", err)
			}
		}

		out := &UnreadOutput{}
		out.Body.Counts = counts
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "mark-chat-read",
		Method:      "POST",
		Path:        "/api/portal/chat/read",
		Summary:     "Record that the caller has read a thread up to a message id",
	}, func(ctx context.Context, in *MarkReadInput) (*ActionOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		key := in.Body.Key
		if len(key) > 200 || in.Body.MessageID <= 0 ||
			!(strings.HasPrefix(key, "group:") || strings.HasPrefix(key, "custom:") || strings.HasPrefix(key, "dm:")) {
			return nil, huma.Error400BadRequest("bad thread key or message id")
		}
		// Only ever moves forward: an older tab marking an earlier message
		// must not un-read what another tab already read.
		_, err = s.DB.Exec(ctx, `
			INSERT INTO chat_read_state (username, target_key, last_read_id) VALUES ($1, $2, $3)
			ON CONFLICT (username, target_key) DO UPDATE
			SET last_read_id = GREATEST(chat_read_state.last_read_id, EXCLUDED.last_read_id)
		`, username, key, in.Body.MessageID)
		if err != nil {
			return nil, internalError("save read state", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})
}

func (s *Server) addCounts(ctx context.Context, into map[string]int, query string, args ...any) error {
	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return err
		}
		into[key] = n
	}
	return rows.Err()
}
