package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Same reasoning as the wiki module's attachment handling — only genuine
// images ever get served inline, everything else forces a download, and
// the type is sniffed server-side rather than trusted from the upload's
// declared Content-Type (trivially spoofable). See that module for the
// original write-up of why.
var inlineSafeContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func uploadContentType(fileBytes []byte) string {
	n := len(fileBytes)
	if n > 512 {
		n = 512
	}
	return http.DetectContentType(fileBytes[:n])
}

func safeDispositionFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

const maxAttachmentSize = 25 * 1024 * 1024

type UploadAttachmentInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	RawBody      huma.MultipartFormFiles[struct {
		// huma's multipart form fields default to required (unlike JSON
		// body fields, which default to optional) — explicit
		// required:"false" on each of these is load-bearing, not
		// decorative, confirmed by hitting the validation error live.
		GroupName         string        `form:"group_name" required:"false"`
		RecipientUsername string        `form:"recipient_username" required:"false"`
		CustomGroupID     int64         `form:"custom_group_id" required:"false"`
		Caption           string        `form:"caption" required:"false"`
		File              huma.FormFile `form:"file" required:"true"`
	}]
}

type UploadAttachmentOutput struct {
	Body MessageOut
}

type DownloadAttachmentInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	ID           int    `path:"id"`
}

func registerAttachments(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "upload-chat-attachment",
		Method:      "POST",
		Path:        "/api/portal/chat/attachments",
		Summary:     "Send a file to a channel, a DM, or a private group",
	}, func(ctx context.Context, in *UploadAttachmentInput) (*UploadAttachmentOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		data := in.RawBody.Data()
		if targetCount(data.GroupName, data.RecipientUsername, data.CustomGroupID) != 1 {
			return nil, huma.Error400BadRequest("specify exactly one of group_name, recipient_username, or custom_group_id")
		}
		if data.GroupName != "" {
			if err := s.requireChannelMember(ctx, username, data.GroupName); err != nil {
				return nil, err
			}
		}
		if data.CustomGroupID != 0 {
			isMember, err := s.isGroupMember(ctx, data.CustomGroupID, username)
			if err != nil {
				return nil, internalError("check membership", err)
			}
			if !isMember {
				return nil, huma.Error403Forbidden("you're not a member of this group")
			}
		}

		s3, available, err := s.chatS3Client(ctx)
		if err != nil {
			return nil, internalError("check backup storage", err)
		}
		if !available {
			return nil, huma.Error400BadRequest("install the Backup Storage module first — attachments are stored there")
		}

		fileBytes, err := io.ReadAll(io.LimitReader(data.File, maxAttachmentSize+1))
		if err != nil {
			return nil, huma.Error400BadRequest("read upload", err)
		}
		if len(fileBytes) > maxAttachmentSize {
			return nil, huma.Error400BadRequest(fmt.Sprintf("file too large — max %d MB", maxAttachmentSize/1024/1024))
		}

		msg, err := s.insertMessage(ctx, username, data.GroupName, data.RecipientUsername, data.CustomGroupID, data.Caption)
		if err != nil {
			return nil, internalError("save message", err)
		}

		filename := data.File.Filename
		key := fmt.Sprintf("chat/%d/%s", msg.ID, filename)
		if err := s3.Upload(ctx, key, bytes.NewReader(fileBytes), uploadContentType(fileBytes)); err != nil {
			return nil, internalError("upload attachment", err)
		}

		var a AttachmentOut
		err = s.DB.QueryRow(ctx, `
			INSERT INTO chat_attachments (message_id, filename, s3_key, size_bytes, uploaded_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id, filename, size_bytes
		`, msg.ID, filename, key, len(fileBytes), username).Scan(&a.ID, &a.Filename, &a.Size)
		if err != nil {
			return nil, internalError("record attachment", err)
		}
		msg.Attachment = &a

		if err := s.pushMessage(ctx, msg); err != nil {
			return nil, internalError("deliver message", err)
		}

		out := &UploadAttachmentOutput{Body: *msg}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "download-chat-attachment",
		Method:      "GET",
		Path:        "/api/portal/chat/attachments/{id}",
		Summary:     "Download a chat attachment",
	}, func(ctx context.Context, in *DownloadAttachmentInput) (*huma.StreamResponse, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		var filename, key string
		var customGroupID *int64
		err = s.DB.QueryRow(ctx, `
			SELECT a.filename, a.s3_key, m.custom_group_id
			FROM chat_attachments a JOIN chat_messages m ON m.id = a.message_id
			WHERE a.id = $1
		`, in.ID).Scan(&filename, &key, &customGroupID)
		if err != nil {
			return nil, huma.Error404NotFound("no such attachment")
		}
		// Channel/DM attachments have no further access check beyond "is a
		// logged-in employee" — same as before. A private-group attachment
		// is the one case that needs an explicit membership check here,
		// same as the group's messages: without this, anyone who guessed
		// or was sent the raw attachment id could pull a file out of a
		// group they were never added to, defeating the whole point of it
		// being private.
		if customGroupID != nil {
			isMember, err := s.isGroupMember(ctx, *customGroupID, username)
			if err != nil {
				return nil, internalError("check membership", err)
			}
			if !isMember {
				return nil, huma.Error403Forbidden("you're not a member of this group")
			}
		}

		s3, available, err := s.chatS3Client(ctx)
		if err != nil {
			return nil, internalError("check backup storage", err)
		}
		if !available {
			return nil, huma.Error500InternalServerError("attachment storage unavailable", nil)
		}
		body, contentType, err := s3.Download(ctx, key)
		if err != nil {
			return nil, internalError("download attachment", err)
		}

		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				defer body.Close()
				if contentType != "" {
					hctx.SetHeader("Content-Type", contentType)
				}
				hctx.SetHeader("X-Content-Type-Options", "nosniff")
				disposition := "attachment"
				if inlineSafeContentTypes[contentType] {
					disposition = "inline"
				}
				hctx.SetHeader("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, safeDispositionFilename(filename)))
				io.Copy(hctx.BodyWriter(), body)
			},
		}, nil
	})
}
