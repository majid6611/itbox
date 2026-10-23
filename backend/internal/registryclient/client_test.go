package registryclient

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func repoTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf = &bytesBuf{}
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: "itbox-main/" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gw.Close()
	return buf.b
}

type bytesBuf struct{ b []byte }

func (w *bytesBuf) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }

func TestIndexAndBundle(t *testing.T) {
	tarball := repoTarball(t, map[string]string{
		"README.md":                                    "x",
		"modules/fileshare-webdav/manifest.yaml":       "id: fileshare-webdav\nname: File Share\ncategory: storage\nversion: \"1.0.4\"\n",
		"modules/fileshare-webdav/docker-compose.yaml": "services: {}\n",
		"modules/broken/manifest.yaml":                 "id: other\nversion: \"1\"\n", // id != folder: skipped
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/o/r/tar.gz/refs/heads/main" {
			http.NotFound(w, r)
			return
		}
		w.Write(tarball)
	}))
	defer srv.Close()

	c := New("o/r")
	c.BaseURL = srv.URL
	entries, err := c.FetchIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "fileshare-webdav" || entries[0].LatestVersion != "1.0.4" {
		t.Fatalf("unexpected index: %+v", entries)
	}
	data, err := c.DownloadBundle(context.Background(), "fileshare-webdav", "1.0.4", entries[0].SHA256)
	if err != nil || len(data) == 0 {
		t.Fatalf("download: %v", err)
	}
	if _, err := c.DownloadBundle(context.Background(), "fileshare-webdav", "1.0.4", "bad"); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestNotConfigured(t *testing.T) {
	if New("").Configured() {
		t.Fatal("empty repo must be unconfigured")
	}
}
