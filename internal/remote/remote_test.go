package remote

import (
	"errors"
	"strings"
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

func TestManagerPrefersFasterUploadProbeRoute(t *testing.T) {
	host := config.Host{
		Name: "devbox",
		Routes: []config.Route{
			{Name: "public", SSHTarget: "public", Priority: 20},
			{Name: "lan", SSHTarget: "lan", Priority: 10},
		},
	}
	m := NewManager(func(target string) bool { return true })
	m.Tester = func(route config.Route) (time.Duration, bool) {
		switch route.Name {
		case "lan":
			return 80 * time.Millisecond, true
		case "public":
			return 300 * time.Millisecond, true
		default:
			return 0, false
		}
	}

	route := m.BestRoute(host)
	if route.Name != "lan" {
		t.Fatalf("route = %q, want lan", route.Name)
	}
}

func TestManagerKeepsCachedRouteUntilTTLExpires(t *testing.T) {
	host := config.Host{
		Name: "devbox",
		Routes: []config.Route{
			{Name: "public", SSHTarget: "public", Priority: 20},
			{Name: "lan", SSHTarget: "lan", Priority: 10},
		},
	}
	now := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	first := true
	m := NewManager(func(target string) bool { return true })
	m.Now = func() time.Time { return now }
	m.TTL = time.Hour
	m.Tester = func(route config.Route) (time.Duration, bool) {
		if first {
			if route.Name == "public" {
				return 100 * time.Millisecond, true
			}
			return 300 * time.Millisecond, true
		}
		if route.Name == "lan" {
			return 50 * time.Millisecond, true
		}
		return 250 * time.Millisecond, true
	}

	if got := m.BestRoute(host); got.Name != "public" {
		t.Fatalf("initial route = %q, want public", got.Name)
	}
	first = false
	now = now.Add(59 * time.Minute)
	if got := m.BestRoute(host); got.Name != "public" {
		t.Fatalf("cached route = %q, want public", got.Name)
	}
	now = now.Add(2 * time.Minute)
	if got := m.BestRoute(host); got.Name != "lan" {
		t.Fatalf("expired route = %q, want lan", got.Name)
	}
}

func TestManagerInvalidatesHost(t *testing.T) {
	host := config.Host{
		Name: "devbox",
		Routes: []config.Route{
			{Name: "public", SSHTarget: "public", Priority: 20},
			{Name: "lan", SSHTarget: "lan", Priority: 10},
		},
	}
	first := true
	m := NewManager(func(target string) bool { return true })
	m.Tester = func(route config.Route) (time.Duration, bool) {
		if first {
			if route.Name == "public" {
				return 80 * time.Millisecond, true
			}
			return 200 * time.Millisecond, true
		}
		if route.Name == "lan" {
			return 60 * time.Millisecond, true
		}
		return 220 * time.Millisecond, true
	}

	if got := m.BestRoute(host); got.Name != "public" {
		t.Fatalf("initial route = %q, want public", got.Name)
	}
	first = false
	m.InvalidateHost(host.Name)
	if got := m.BestRoute(host); got.Name != "lan" {
		t.Fatalf("route after invalidate = %q, want lan", got.Name)
	}
}

func TestManagerPrefersPriorityWhenProbeTimesAreClose(t *testing.T) {
	host := config.Host{
		Name: "devbox",
		Routes: []config.Route{
			{Name: "public", SSHTarget: "public", Priority: 20},
			{Name: "lan", SSHTarget: "lan", Priority: 10},
		},
	}
	m := NewManager(func(target string) bool { return true })
	m.Tester = func(route config.Route) (time.Duration, bool) {
		if route.Name == "public" {
			return 110 * time.Millisecond, true
		}
		return 130 * time.Millisecond, true
	}

	if got := m.BestRoute(host); got.Name != "lan" {
		t.Fatalf("route = %q, want priority route lan", got.Name)
	}
}

func TestUploaderCanRetryWithFreshRoute(t *testing.T) {
	host := config.Host{Name: "devbox"}
	public := config.Route{Name: "public", SSHTarget: "public"}
	lan := config.Route{Name: "lan", SSHTarget: "lan"}
	attempts := 0
	u := Uploader{
		Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
		Runner: func(data []byte, target, remotePath string) error {
			attempts++
			if attempts == 1 && target == "public" {
				return errors.New("stale route")
			}
			if target != "lan" {
				t.Fatalf("retry target = %q, want lan", target)
			}
			return nil
		},
	}

	path, route, err := u.UploadWithRetry([]byte("png"), "dev", host, public, "png", func() config.Route { return lan })
	if err != nil {
		t.Fatal(err)
	}
	if route.Name != "lan" || attempts != 2 {
		t.Fatalf("route=%+v attempts=%d", route, attempts)
	}
	if path != "/tmp/clipport/dev/clipboard-20260428-121541.000000.png" {
		t.Fatalf("path = %q", path)
	}
}

func TestSSHUploadCommandUsesBoundedNonInteractiveOptions(t *testing.T) {
	cmd := sshCatUploadCommand([]byte("png"), "devbox", "/tmp/clipport/dev/a.png")
	assertArgsContain(t, cmd.Args,
		"-o\nPermitLocalCommand=no",
		"-o\nClearAllForwardings=yes",
		"-o\nBatchMode=yes",
		"-o\nConnectTimeout=3",
		"-o\nConnectionAttempts=1",
		"-o\nServerAliveInterval=5",
		"-o\nServerAliveCountMax=1",
		"-o\nControlMaster=no",
	)
}

func TestForwardHealthCommandChecksRemoteForwardWithRemoteToken(t *testing.T) {
	cmd := forwardHealthCommand("devbox", "127.0.0.1:18765")
	assertArgsContain(t, cmd.Args,
		"-o\nPermitLocalCommand=no",
		"-o\nClearAllForwardings=yes",
		"-o\nBatchMode=yes",
		"-o\nConnectTimeout=2",
		"devbox",
		"cat \"$HOME/.config/clipport/token\"",
		"http://127.0.0.1:18765/v1/health",
	)
}

func assertArgsContain(t *testing.T, args []string, wants ...string) {
	t.Helper()
	text := strings.Join(args, "\n")
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("args missing %q in:\n%s", want, text)
		}
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
