package docker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Needs a Docker daemon: TEST_DOCKER=1 go test ./internal/docker
func TestProjectLogsMergesStdoutAndStderr(t *testing.T) {
	if os.Getenv("TEST_DOCKER") == "" {
		t.Skip("TEST_DOCKER not set")
	}
	ctx := context.Background()
	for _, name := range []string{"itlogtest-a", "itlogtest-b"} {
		exec.Command("docker", "rm", "-f", name).Run()
		out, err := exec.Command("docker", "run", "-d", "--name", name,
			"--label", "com.docker.compose.project=itlogtest", "alpine",
			"sh", "-c", "echo out-from-"+name+"; echo err-from-"+name+" >&2; sleep 30").CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %s", err, out)
		}
		defer exec.Command("docker", "rm", "-f", name).Run()
	}
	time.Sleep(time.Second)

	got, err := (&Client{}).ProjectLogs(ctx, "itlogtest", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[itlogtest-a] out-from-itlogtest-a", "[itlogtest-a] err-from-itlogtest-a", "[itlogtest-b] out-from-itlogtest-b"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	short, _ := (&Client{}).ProjectLogs(ctx, "itlogtest", 2)
	if n := strings.Count(short, "\n"); n != 2 {
		t.Errorf("tail=2 returned %d lines:\n%s", n, short)
	}
	if none, err := (&Client{}).ProjectLogs(ctx, "no-such-project", 10); err != nil || none != "" {
		t.Errorf("unknown project: %q %v", none, err)
	}
}
