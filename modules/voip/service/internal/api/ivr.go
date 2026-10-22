package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/voip/internal/pbx"
)

const (
	greetingFile      = "greeting.wav"
	maxGreetingBytes  = 20 * 1024 * 1024
	maxGreetingSecs   = 120
	maxMenuKeys       = 9
	maxTargetsPerKey  = 10
	convertTimeout    = 40 * time.Second
	greetingMinBytes  = 1000
	greetingDialCode  = "*99"
	greetingFormatMsg = "that doesn't look like an audio file — use WAV, MP3, M4A, OGG, FLAC or WebM"
)

type MenuOption struct {
	Digit   string   `json:"digit"`
	Label   string   `json:"label"`
	Targets []string `json:"targets"`
}

type Menu struct {
	Enabled bool `json:"enabled"`
	// Recorder is the extension allowed to re-record the greeting by phone.
	Recorder    string       `json:"recorder"`
	RecordCode  string       `json:"record_code"`
	HasGreeting bool         `json:"has_greeting"`
	GreetingAt  string       `json:"greeting_updated_at,omitempty"`
	Options     []MenuOption `json:"options"`
}

type GetMenuInput struct {
	SessionToken string `cookie:"itp_session"`
}

type MenuOutput struct {
	Body Menu
}

type SaveMenuInput struct {
	SessionToken string `cookie:"itp_session"`
	Body         struct {
		Enabled  bool         `json:"enabled"`
		Recorder string       `json:"recorder"`
		Options  []MenuOption `json:"options"`
	}
}

type UploadGreetingInput struct {
	SessionToken string `cookie:"itp_session"`
	RawBody      huma.MultipartFormFiles[struct {
		File huma.FormFile `form:"file" required:"true"`
	}]
}

type PlayGreetingInput struct {
	SessionToken string `cookie:"itp_session"`
}

func (s *Server) greetingPath() string { return filepath.Join(s.SoundsDir, greetingFile) }

// greetingInfo says whether a greeting is recorded and when it last changed.
func (s *Server) greetingInfo() (ok bool, at time.Time) {
	fi, err := os.Stat(s.greetingPath())
	if err != nil || fi.Size() < greetingMinBytes {
		return false, time.Time{}
	}
	return true, fi.ModTime()
}

func (s *Server) loadMenuOptions(ctx context.Context) ([]MenuOption, error) {
	rows, err := s.DB.Query(ctx, `SELECT digit, label, targets FROM voip_ivr_options ORDER BY digit`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	opts := []MenuOption{}
	for rows.Next() {
		var o MenuOption
		var targets string
		if err := rows.Scan(&o.Digit, &o.Label, &targets); err != nil {
			return nil, err
		}
		for _, t := range strings.Split(targets, ",") {
			if t != "" {
				o.Targets = append(o.Targets, t)
			}
		}
		opts = append(opts, o)
	}
	return opts, rows.Err()
}

// loadIVR is what the dialplan generator needs; Apply calls it.
func (s *Server) loadIVR(ctx context.Context) (pbx.IVR, error) {
	var ivr pbx.IVR
	if err := s.DB.QueryRow(ctx, `SELECT ivr_enabled, ivr_recorder FROM voip_settings WHERE id = true`).Scan(&ivr.Enabled, &ivr.Recorder); err != nil {
		return ivr, err
	}
	opts, err := s.loadMenuOptions(ctx)
	if err != nil {
		return ivr, err
	}
	for _, o := range opts {
		ivr.Options = append(ivr.Options, pbx.IVROption{Digit: o.Digit, Label: o.Label, Targets: o.Targets})
	}
	ivr.HasGreeting, _ = s.greetingInfo()
	return ivr, nil
}

// audioDemuxer picks the ffmpeg input format from the file's own first
// bytes, and refuses anything that isn't plain audio. Naming the format
// explicitly (instead of letting ffmpeg guess) keeps playlist-style inputs,
// which can point at other files or addresses, from ever being opened.
func audioDemuxer(head []byte) (string, bool) {
	switch {
	case len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		return "wav", true
	case len(head) >= 3 && string(head[0:3]) == "ID3", len(head) >= 2 && head[0] == 0xFF && head[1]&0xE0 == 0xE0:
		return "mp3", true
	case len(head) >= 4 && string(head[0:4]) == "OggS":
		return "ogg", true
	case len(head) >= 4 && string(head[0:4]) == "fLaC":
		return "flac", true
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return "mov", true // m4a / mp4 audio
	case len(head) >= 4 && head[0] == 0x1A && head[1] == 0x45 && head[2] == 0xDF && head[3] == 0xA3:
		return "matroska", true // webm
	}
	return "", false
}

// convertGreeting turns an uploaded audio file into what Asterisk plays:
// 8 kHz, mono, 16-bit WAV, at most two minutes. The new file only replaces
// the old one once the conversion has succeeded.
func (s *Server) convertGreeting(ctx context.Context, data []byte) error {
	format, ok := audioDemuxer(data)
	if !ok {
		return fmt.Errorf(greetingFormatMsg)
	}
	if err := os.MkdirAll(s.SoundsDir, 0o777); err != nil {
		return err
	}
	// Asterisk runs as another user and needs to write here (recording by
	// phone), so the volume's directory must be open to it.
	_ = os.Chmod(s.SoundsDir, 0o777)

	in, err := os.CreateTemp(s.SoundsDir, ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(in.Name())
	if _, err := io.Copy(in, bytes.NewReader(data)); err != nil {
		in.Close()
		return err
	}
	in.Close()

	out := filepath.Join(s.SoundsDir, ".converted.wav")
	defer os.Remove(out)
	cctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "ffmpeg", "-nostdin", "-y", "-loglevel", "error",
		"-f", format, "-i", in.Name(),
		"-vn", "-t", fmt.Sprint(maxGreetingSecs), "-ar", "8000", "-ac", "1", "-c:a", "pcm_s16le", "-f", "wav", out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("couldn't read that audio file: %s", strings.TrimSpace(string(msg)))
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() < greetingMinBytes {
		return fmt.Errorf("that recording is empty or too short")
	}
	// Writable by Asterisk too (it runs as another user): re-recording by
	// phone opens this file for writing.
	if err := os.Chmod(out, 0o666); err != nil {
		return err
	}
	return os.Rename(out, s.greetingPath())
}

func (s *Server) extensionNumbers(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.Query(ctx, `SELECT number FROM voip_extensions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		set[n] = true
	}
	return set, rows.Err()
}

func registerIVR(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "get-voip-menu",
		Method:      "GET",
		Path:        "/api/voip/menu",
		Summary:     "Get the phone menu (\"press 1 for ...\") that outside callers hear (admin only)",
	}, func(ctx context.Context, in *GetMenuInput) (*MenuOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		ivr, err := s.loadIVR(ctx)
		if err != nil {
			return nil, internalError("load menu", err)
		}
		opts, err := s.loadMenuOptions(ctx)
		if err != nil {
			return nil, internalError("load menu keys", err)
		}
		out := &MenuOutput{}
		out.Body = Menu{Enabled: ivr.Enabled, Recorder: ivr.Recorder, RecordCode: greetingDialCode, HasGreeting: ivr.HasGreeting, Options: opts}
		if ok, at := s.greetingInfo(); ok {
			out.Body.GreetingAt = at.UTC().Format(time.RFC3339)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "save-voip-menu",
		Method:      "PUT",
		Path:        "/api/voip/menu",
		Summary:     "Save the phone menu: which key rings which extensions (admin only)",
	}, func(ctx context.Context, in *SaveMenuInput) (*ActionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		b := in.Body
		known, err := s.extensionNumbers(ctx)
		if err != nil {
			return nil, internalError("list extensions", err)
		}
		if b.Recorder != "" && !known[b.Recorder] {
			return nil, huma.Error400BadRequest("the extension chosen for recording doesn't exist")
		}
		if len(b.Options) > maxMenuKeys {
			return nil, huma.Error400BadRequest("a menu can have at most 9 keys")
		}
		seen := map[string]bool{}
		for i := range b.Options {
			o := &b.Options[i]
			o.Label = strings.TrimSpace(o.Label)
			if !pbx.ValidDigit(o.Digit) {
				return nil, huma.Error400BadRequest("each key must be a digit from 1 to 9 (0 always reaches the main group)")
			}
			if seen[o.Digit] {
				return nil, huma.Error400BadRequest("key " + o.Digit + " is used twice")
			}
			seen[o.Digit] = true
			if !pbx.ValidName(o.Label) {
				return nil, huma.Error400BadRequest("each key needs a short name (letters, numbers, spaces — up to 40)")
			}
			if len(o.Targets) == 0 || len(o.Targets) > maxTargetsPerKey {
				return nil, huma.Error400BadRequest("key " + o.Digit + " needs between 1 and 10 extensions to ring")
			}
			for _, t := range o.Targets {
				if !known[t] {
					return nil, huma.Error400BadRequest("key " + o.Digit + " points at extension " + t + ", which doesn't exist")
				}
			}
		}
		if b.Enabled {
			if has, _ := s.greetingInfo(); !has {
				return nil, huma.Error400BadRequest("record or upload the greeting before turning the menu on")
			}
			if len(b.Options) == 0 {
				return nil, huma.Error400BadRequest("add at least one key before turning the menu on")
			}
		}

		tx, err := s.DB.Begin(ctx)
		if err != nil {
			return nil, internalError("begin", err)
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `UPDATE voip_settings SET ivr_enabled = $1, ivr_recorder = $2 WHERE id = true`, b.Enabled, b.Recorder); err != nil {
			return nil, internalError("save menu", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM voip_ivr_options`); err != nil {
			return nil, internalError("clear keys", err)
		}
		sort.Slice(b.Options, func(i, j int) bool { return b.Options[i].Digit < b.Options[j].Digit })
		for _, o := range b.Options {
			if _, err := tx.Exec(ctx, `INSERT INTO voip_ivr_options (digit, label, targets) VALUES ($1, $2, $3)`, o.Digit, o.Label, strings.Join(o.Targets, ",")); err != nil {
				return nil, internalError("save key", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, internalError("commit", err)
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "upload-voip-greeting",
		Method:      "POST",
		Path:        "/api/voip/menu/greeting",
		Summary:     "Upload the greeting callers hear (any common audio file; admin only)",
	}, func(ctx context.Context, in *UploadGreetingInput) (*ActionOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(in.RawBody.Data().File, maxGreetingBytes+1))
		if err != nil {
			return nil, huma.Error400BadRequest("read upload", err)
		}
		if len(data) > maxGreetingBytes {
			return nil, huma.Error400BadRequest("that file is too big (20 MB at most)")
		}
		if err := s.convertGreeting(ctx, data); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if err := s.Apply(ctx); err != nil {
			return nil, internalError("apply config", err)
		}
		out := &ActionOutput{}
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "play-voip-greeting",
		Method:      "GET",
		Path:        "/api/voip/menu/greeting",
		Summary:     "Listen to the current greeting (admin only)",
	}, func(ctx context.Context, in *PlayGreetingInput) (*huma.StreamResponse, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		f, err := os.Open(s.greetingPath())
		if err != nil {
			return nil, huma.Error404NotFound("no greeting recorded yet")
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer f.Close()
			hctx.SetHeader("Content-Type", "audio/wav")
			hctx.SetHeader("Cache-Control", "no-store")
			io.Copy(hctx.BodyWriter(), f)
		}}, nil
	})
}
