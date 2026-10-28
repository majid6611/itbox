package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestScrubHidesEmails(t *testing.T) {
	got := scrub("Device removed: laptop (anna.k@firm.example.com) by bob@x.io")
	if strings.Contains(got, "@") {
		t.Fatalf("email survived: %q", got)
	}
}

func TestLicenceValid(t *testing.T) {
	if licenceValid("") || !licenceValid("ABC-123") {
		t.Fatal("empty key must not count, any other key does for now")
	}
}

func TestMonitorEnabledNeedsEverything(t *testing.T) {
	on := licenceRow{Key: "k", SendMonitoring: true}
	cases := []struct {
		url  string
		l    licenceRow
		want bool
	}{
		{"https://m.example", on, true},
		{"off", on, false},
		{"", on, false},
		{"https://m.example", licenceRow{Key: "", SendMonitoring: true}, false},
		{"https://m.example", licenceRow{Key: "k", SendMonitoring: false}, false},
	}
	for _, c := range cases {
		if got := (&Server{MonitorURL: c.url}).monitorEnabled(c.l); got != c.want {
			t.Errorf("url=%q licence=%+v: got %v want %v", c.url, c.l, got, c.want)
		}
	}
}

func TestPostSendsKeyAndPayload(t *testing.T) {
	var gotAuth, gotID, gotPath string
	var got Report
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotID, gotPath = r.Header.Get("Authorization"), r.Header.Get("X-Install-ID"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	s := &Server{MonitorURL: srv.URL + "/"}
	err := s.post(context.Background(), licenceRow{InstallID: "inst-1", Key: "KEY"}, &Report{InstallID: "inst-1", Logs: []reportLog{{ID: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer KEY" || gotID != "inst-1" || gotPath != "/api/report" || got.InstallID != "inst-1" || len(got.Logs) != 1 {
		t.Fatalf("auth=%q id=%q path=%q report=%+v", gotAuth, gotID, gotPath, got)
	}
}

func TestPostReportsRejectedLicence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer srv.Close()
	err := (&Server{MonitorURL: srv.URL}).post(context.Background(), licenceRow{Key: "K"}, &Report{})
	if err == nil || !strings.Contains(err.Error(), "rejected the licence") {
		t.Fatalf("got %v", err)
	}
}
