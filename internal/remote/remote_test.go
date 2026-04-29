package remote

import (
	"testing"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
)

func TestManagerChoosesFirstHealthyRoute(t *testing.T) {
	host := config.Host{
		Name: "devbox",
		Routes: []config.Route{
			{Name: "public", SSHTarget: "public", Priority: 20},
			{Name: "lan", SSHTarget: "lan", Priority: 10},
		},
	}
	m := NewManager(func(target string) bool { return target == "public" })
	route := m.BestRoute(host)
	if route.Name != "public" {
		t.Fatalf("route = %q, want public", route.Name)
	}
}

func TestRemotePathIsDeterministicUnderTmp(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 15, 41, 123456000, time.UTC)
	got := RemotePath("dev", "png", now)
	want := "/tmp/clipport/dev/clipboard-20260428-121541.123456.png"
	if got != want {
		t.Fatalf("RemotePath() = %q, want %q", got, want)
	}
}

func TestUploaderReturnsRemotePath(t *testing.T) {
	host := config.Host{Name: "devbox"}
	route := config.Route{Name: "public", SSHTarget: "devbox-public"}
	u := Uploader{
		Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
		Runner: func(data []byte, target, remotePath string) error {
			if string(data) != "png" || target != "devbox-public" {
				t.Fatalf("upload args = %q %q %q", string(data), target, remotePath)
			}
			return nil
		},
	}
	got, err := u.Upload([]byte("png"), "dev", host, route, "png")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/clipport/dev/clipboard-20260428-121541.000000.png" {
		t.Fatalf("path = %q", got)
	}
}

func TestUploaderUsesRequestedExtension(t *testing.T) {
	host := config.Host{Name: "devbox"}
	route := config.Route{Name: "public", SSHTarget: "devbox-public"}
	u := Uploader{
		Now:    func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
		Runner: func(data []byte, target, remotePath string) error { return nil },
	}
	got, err := u.Upload([]byte("text"), "dev", host, route, "txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/clipport/dev/clipboard-20260428-121541.000000.txt" {
		t.Fatalf("path = %q", got)
	}
}
