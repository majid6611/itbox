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

// inlineSafeContentTypes are the only types ever served with
// Content-Disposition: inline — everything else forces a download instead
// of letting the browser render it, so an uploaded HTML/SVG file can never
// execute as a page in this origin (which would run with the viewer's own
// itp_employee_session cookie in scope). Deliberately checked against the
// server-sniffed type (see uploadContentType below), never whatever
// Content-Type the uploading client claimed — that header is trivial to
// spoof on a multipart upload.
var inlineSafeContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// uploadContentType sniffs the real content type from the file's own
// bytes rather than trusting the client-supplied Content-Type — a
// malicious uploader can set that header to anything regardless of what's
// actually in the file.
func uploadContentType(fileBytes []byte) string {
	n := len(fileBytes)
	if n > 512 {
		n = 512
	}
	return http.DetectContentType(fileBytes[:n])
}

// safeDispositionFilename strips characters that could break out of the
// quoted Content-Disposition filename parameter (or, pre-Go's built-in CR/LF
// header rejection, inject additional response headers).
func safeDispositionFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// maxAttachmentSize caps a single wiki attachment upload — without this,
// io.ReadAll(data.File) below reads the entire upload into memory with no
// limit at all, so any logged-in employee could exhaust backend memory
// with a handful of huge uploads. 25 MB comfortably covers real wiki
// attachments (images, docs) without needing a config knob for it.
const maxAttachmentSize = 25 * 1024 * 1024

type UploadWikiAttachmentInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	RawBody      huma.MultipartFormFiles[struct {
		Path string        `form:"path" required:"true"`
		File huma.FormFile `form:"file" required:"true"`
	}]
}

type UploadWikiAttachmentOutput struct {
	Body WikiAttachment
}

type ListWikiAttachmentsInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	Path         string `query:"path"`
}

type ListWikiAttachmentsOutput struct {
	Body struct {
		Attachments []WikiAttachment `json:"attachments"`
	}
}

type DownloadWikiAttachmentInput struct {
	SessionToken string `cookie:"itp_employee_session"`
	ID           int    `path:"id"`
}

func registerWikiAttachments(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "upload-wiki-attachment",
		Method:      "POST",
		Path:        "/api/portal/wiki/page/attachments",
		Summary:     "Upload a file attached to a wiki page",
	}, func(ctx context.Context, in *UploadWikiAttachmentInput) (*UploadWikiAttachmentOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		data := in.RawBody.Data()
		group, _ := s.employeeGroup(ctx, username)

		pageID, _, ok, err := s.getPageByPath(ctx, data.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canWrite, err := s.wikiPageAccess(ctx, pageID, group, false, true)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canWrite {
			return nil, huma.Error403Forbidden("you don't have write access to this page")
		}

		s3, available, err := s.wikiS3Client(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check backup storage", err)
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
		filename := data.File.Filename
		key := fmt.Sprintf("wiki/%d/%s", pageID, filename)
		if err := s3.Upload(ctx, key, bytes.NewReader(fileBytes), uploadContentType(fileBytes)); err != nil {
			return nil, huma.Error500InternalServerError("upload attachment", err)
		}

		var a WikiAttachment
		err = s.DB.QueryRow(ctx, `
			INSERT INTO wiki_attachments (page_id, filename, s3_key, size_bytes, uploaded_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id, filename, size_bytes
		`, pageID, filename, key, len(fileBytes), username).Scan(&a.ID, &a.Filename, &a.Size)
		if err != nil {
			return nil, huma.Error500InternalServerError("record attachment", err)
		}

		out := &UploadWikiAttachmentOutput{Body: a}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-wiki-attachments",
		Method:      "GET",
		Path:        "/api/portal/wiki/page/attachments",
		Summary:     "List a page's attachments",
	}, func(ctx context.Context, in *ListWikiAttachmentsInput) (*ListWikiAttachmentsOutput, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		pageID, _, ok, err := s.getPageByPath(ctx, in.Path)
		if err != nil {
			return nil, huma.Error500InternalServerError("load page", err)
		}
		if !ok {
			return nil, huma.Error404NotFound("no such page")
		}
		canRead, err := s.wikiPageAccess(ctx, pageID, group, false, false)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}

		rows, err := s.DB.Query(ctx, `SELECT id, filename, size_bytes FROM wiki_attachments WHERE page_id = $1 ORDER BY uploaded_at DESC`, pageID)
		if err != nil {
			return nil, huma.Error500InternalServerError("list attachments", err)
		}
		defer rows.Close()
		out := &ListWikiAttachmentsOutput{}
		for rows.Next() {
			var a WikiAttachment
			if err := rows.Scan(&a.ID, &a.Filename, &a.Size); err != nil {
				return nil, huma.Error500InternalServerError("scan attachment", err)
			}
			out.Body.Attachments = append(out.Body.Attachments, a)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "download-wiki-attachment",
		Method:      "GET",
		Path:        "/api/portal/wiki/attachments/{id}",
		Summary:     "Download a wiki attachment",
	}, func(ctx context.Context, in *DownloadWikiAttachmentInput) (*huma.StreamResponse, error) {
		username, err := s.requireEmployeeAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		group, _ := s.employeeGroup(ctx, username)

		var pageID int
		var filename string
		var key string
		err = s.DB.QueryRow(ctx, `SELECT page_id, filename, s3_key FROM wiki_attachments WHERE id = $1`, in.ID).Scan(&pageID, &filename, &key)
		if err != nil {
			return nil, huma.Error404NotFound("no such attachment")
		}
		canRead, err := s.wikiPageAccess(ctx, pageID, group, false, false)
		if err != nil {
			return nil, huma.Error500InternalServerError("check access", err)
		}
		if !canRead {
			return nil, huma.Error403Forbidden("you don't have access to this page")
		}

		s3, available, err := s.wikiS3Client(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("check backup storage", err)
		}
		if !available {
			return nil, huma.Error500InternalServerError("attachment storage unavailable", nil)
		}
		body, contentType, err := s3.Download(ctx, key)
		if err != nil {
			return nil, huma.Error500InternalServerError("download attachment", err)
		}

		return &huma.StreamResponse{
			Body: func(hctx huma.Context) {
				defer body.Close()
				if contentType != "" {
					hctx.SetHeader("Content-Type", contentType)
				}
				// nosniff so a browser never second-guesses the declared
				// type for a forced download and decides to render it
				// inline anyway; disposition itself is the real control —
				// only a known-safe image type ever gets "inline", so an
				// uploaded HTML/SVG/etc. file always downloads instead of
				// executing in this origin.
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
