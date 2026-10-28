package api

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
)

type SearchInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Q            string `query:"q"`
}

type SearchOutput struct {
	Body struct {
		Messages []MessageOut `json:"messages"`
	}
}

const (
	minSearchChars = 2
	maxSearchChars = 100
	searchLimit    = 30
)

func registerSearch(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "search-chat-messages",
		Method:      "GET",
		Path:        "/api/portal/chat/search",
		Summary:     "Search the messages the caller can see — newest matches first",
	}, func(ctx context.Context, in *SearchInput) (*SearchOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		q := strings.TrimSpace(in.Q)
		if n := utf8.RuneCountInString(q); n < minSearchChars || n > maxSearchChars {
			return nil, huma.Error400BadRequest("search needs 2 to 100 characters")
		}
		group, err := s.employeeGroup(ctx, username)
		if err != nil {
			return nil, internalError("look up group", err)
		}

		// The visibility rule is the same one history enforces, written as a
		// filter: your own LDAP group's channel, private groups you're a
		// member of, and DMs you sent or received. Anything else never
		// reaches the result set.
		rows, err := s.DB.Query(ctx, `
			SELECT id, sender_username, group_name, recipient_username, custom_group_id, content, created_at, edited_at, deleted_at
			FROM chat_messages
			WHERE deleted_at IS NULL
			  AND to_tsvector('simple', content) @@ websearch_to_tsquery('simple', $1)
			  AND (
			    (group_name IS NOT NULL AND group_name = $3)
			    OR custom_group_id IN (SELECT group_id FROM chat_group_members WHERE username = $2)
			    OR (recipient_username IS NOT NULL AND (sender_username = $2 OR recipient_username = $2))
			  )
			ORDER BY id DESC LIMIT `+strconv.Itoa(searchLimit), q, username, group)
		if err != nil {
			return nil, internalError("search messages", err)
		}
		messages, err := s.collectMessages(ctx, rows)
		if err != nil {
			return nil, internalError("search messages", err)
		}
		out := &SearchOutput{}
		out.Body.Messages = messages
		return out, nil
	})
}
