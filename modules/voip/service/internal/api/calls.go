package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Only the tail of the call log is ever read — it grows forever and the
// panel shows the most recent calls, so there's no reason to parse years
// of history on every page load.
const (
	cdrTailBytes = 256 * 1024
	maxCalls     = 200
)

type ListCallsInput struct {
	SessionToken string `cookie:"itp_session"`
}

type ListCallsOutput struct {
	Body struct {
		Calls []Call `json:"calls"`
	}
}

// readRecentCalls parses Asterisk's cdr_csv Master.csv, newest first.
// Column order is cdr_csv's fixed default (loguniqueid/loguserfield off,
// see the asterisk image's cdr.conf): accountcode, src, dst, dcontext,
// clid, channel, dstchannel, lastapp, lastdata, start, answer, end,
// duration, billsec, disposition, amaflags.
func readRecentCalls(path string) ([]Call, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Call{}, nil // no calls made yet
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	partial := false
	if info.Size() > cdrTailBytes {
		if _, err := f.Seek(-cdrTailBytes, io.SeekEnd); err != nil {
			return nil, err
		}
		partial = true
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if partial {
		// Drop the line the seek landed in the middle of.
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	var calls []Call
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 15 {
			continue // a malformed or half-written line — skip it, not the whole log
		}
		calls = append(calls, toCall(rec))
	}
	// Newest first, capped.
	for i, j := 0, len(calls)-1; i < j; i, j = i+1, j-1 {
		calls[i], calls[j] = calls[j], calls[i]
	}
	if len(calls) > maxCalls {
		calls = calls[:maxCalls]
	}
	if calls == nil {
		calls = []Call{}
	}
	return calls, nil
}

func toCall(rec []string) Call {
	src, dst, dcontext, clid := rec[1], rec[2], rec[3], rec[4]
	c := Call{From: callerLabel(clid, src)}

	switch {
	case dcontext == "itp-main" || dcontext == "itp-incoming" || dst == "0":
		c.To = "Main number"
	case dst == "*97" || dst == "*98":
		c.To = "Voicemail"
	case dst == "*43":
		c.To = "Echo test"
	case dcontext == "itp-internal" && strings.HasPrefix(dst, "9") && len(dst) > 2:
		c.To = dst[1:] + " (outside)"
	default:
		c.To = dst
	}

	if t, err := time.Parse("2006-01-02 15:04:05", rec[9]); err == nil {
		c.Start = t.UTC().Format(time.RFC3339) // cdr.conf: usegmtime = yes
	}
	c.TalkSeconds, _ = strconv.Atoi(rec[13])

	switch {
	case rec[14] == "ANSWERED" && rec[6] == "" && !strings.HasPrefix(dst, "*"):
		// Answered with no destination phone at all — i.e. by Asterisk
		// itself, which outside the * service codes only ever means
		// voicemail picked up. (lastapp can't tell us: CDR keeps "Dial"
		// there even when VoiceMail ran after it.)
		c.Result = "voicemail"
	case rec[14] == "ANSWERED":
		c.Result = "answered"
	case rec[14] == "NO ANSWER":
		c.Result = "missed"
	case rec[14] == "BUSY":
		c.Result = "busy"
	default:
		c.Result = "failed"
	}
	return c
}

// callerLabel turns `"Alice" <101>` into "Alice (101)".
func callerLabel(clid, src string) string {
	name := clid
	if i := strings.Index(clid, "<"); i >= 0 {
		name = clid[:i]
	}
	name = strings.TrimSpace(strings.Trim(strings.TrimSpace(name), `"`))
	switch {
	case name != "" && src != "" && name != src:
		return name + " (" + src + ")"
	case src != "":
		return src
	default:
		return name
	}
}

func registerCalls(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "list-voip-calls",
		Method:      "GET",
		Path:        "/api/voip/calls",
		Summary:     "Most recent calls, newest first (admin only)",
	}, func(ctx context.Context, in *ListCallsInput) (*ListCallsOutput, error) {
		if err := s.requireAdminAuth(ctx, in.SessionToken); err != nil {
			return nil, err
		}
		calls, err := readRecentCalls(s.CDRFile)
		if err != nil {
			return nil, internalError("read call log", err)
		}
		out := &ListCallsOutput{}
		out.Body.Calls = calls
		return out, nil
	})
}
