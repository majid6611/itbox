package api

import (
	"strings"
	"testing"
)

func TestRedactMasksCredentials(t *testing.T) {
	in := strings.Join([]string{
		`2026-10-23T10:00:00Z [wiki] starting`,
		`password=hunter2 user=bob`,
		`ADMIN_PASSWORD: "s3cret value"`,
		`Authorization: Bearer abc.def.ghi`,
		`netbird up --setup-key 8F2A-11EE-AAAA`,
		`{"access_key":"GKabc123","region":"garage"}`,
	}, "\n")
	out := redact(in)
	for _, leak := range []string{"hunter2", "s3cret", "abc.def.ghi", "GKabc123", "8F2A-11EE"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	for _, keep := range []string{"starting", "user=bob", `"region":"garage"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q was wrongly removed:\n%s", keep, out)
		}
	}
}
