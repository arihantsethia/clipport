package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
)

func TestCheckConfiguredRemoteForwardDistinguishesOptionalFromBroken(t *testing.T) {
	oldCheck := checkForwardHealth
	t.Cleanup(func() { checkForwardHealth = oldCheck })
	for _, tt := range []struct {
		name, settings, addr, detail string
		healthErr                    error
		skip, ok                     bool
	}{
		{name: "not configured", skip: true, ok: true},
		{name: "unrelated forwarding", settings: "RemoteForward 127.0.0.1:9000 127.0.0.1:9000", skip: true, ok: true},
		{name: "local forwarding", settings: "LocalForward 127.0.0.1:18765 localhost:18765", skip: true, ok: true},
		{name: "configured healthy", settings: "RemoteForward 127.0.0.1:18765 127.0.0.1:18765", addr: "127.0.0.1:18765", ok: true},
		{name: "configured broken", settings: "RemoteForward 127.0.0.1:18765 127.0.0.1:18765", addr: "127.0.0.1:18765", healthErr: errors.New("connection refused"), detail: "connection refused"},
		{name: "localhost", settings: "RemoteForward localhost:18765 localhost:18765", addr: "localhost:18765", ok: true},
		{name: "IPv6", settings: "RemoteForward [::1]:18765 [::1]:18765", addr: "[::1]:18765", ok: true},
		{name: "port only", settings: "RemoteForward 18765 localhost:18765", addr: "localhost:18765", ok: true},
		{name: "custom remote port", settings: "RemoteForward localhost:19000 127.0.0.1:18765", addr: "localhost:19000", ok: true},
		{name: "custom IPv6 port", settings: "RemoteForward [::1]:19000 localhost:18765", addr: "[::1]:19000", ok: true},
		{name: "custom port only", settings: "RemoteForward 19000 localhost:18765", addr: "localhost:19000", ok: true},
		{name: "wildcard listener", settings: "RemoteForward 0.0.0.0:18765 localhost:18765", addr: "127.0.0.1:18765", ok: true},
		{name: "misdirected wrong port", settings: "RemoteForward 127.0.0.1:18765 127.0.0.1:9000", detail: "forwarding conflict"},
		{name: "misdirected external host", settings: "RemoteForward 127.0.0.1:18765 attacker.example:80", detail: "forwarding conflict"},
		{name: "misdirected external same port", settings: "RemoteForward 127.0.0.1:19000 attacker.example:18765", detail: "forwarding conflict"},
		{name: "misdirected external IP", settings: "RemoteForward 127.0.0.1:18765 198.51.100.1:18765", detail: "forwarding conflict"},
		{name: "misdirected wildcard destination", settings: "RemoteForward 127.0.0.1:18765 0.0.0.0:18765", detail: "forwarding conflict"},
		{name: "misdirected duplicate rule", settings: "RemoteForward 127.0.0.1:18765 127.0.0.1:18765\nRemoteForward 127.0.0.1:18765 attacker.example:80", detail: "forwarding conflict"},
		{name: "non-loopback listener", settings: "RemoteForward 198.51.100.1:18765 localhost:18765", detail: "non-loopback remote address"},
		{name: "inspection failed", settings: "InvalidClipportOption yes", detail: "cannot inspect SSH forwarding configuration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ssh_config")
			if err := os.WriteFile(path, []byte("Host devbox\n"+tt.settings+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			called := false
			checkForwardHealth = func(target, addr string) error {
				called = true
				if target != "devbox" || addr != tt.addr {
					t.Fatalf("health target=%q addr=%q, want devbox %q", target, addr, tt.addr)
				}
				return tt.healthErr
			}
			check := checkForward("dev", config.Route{Name: "public", SSHTarget: "devbox"}, "127.0.0.1:18765", path)
			if check.OK != tt.ok || check.Skipped != tt.skip || called != (tt.addr != "") {
				t.Fatalf("check=%+v called=%v", check, called)
			}
			if tt.detail != "" && !strings.Contains(check.Detail, tt.detail) {
				t.Fatalf("detail %q missing %q", check.Detail, tt.detail)
			}
		})
	}
}

func TestCheckRemoteForwardReportsReceivingSideHealth(t *testing.T) {
	previous := checkForwardHealth
	t.Cleanup(func() { checkForwardHealth = previous })
	checkForwardHealth = func(target, addr string) error {
		if target != "devbox" || addr != "127.0.0.1:18765" {
			t.Fatalf("target=%q addr=%q", target, addr)
		}
		return nil
	}

	check := checkRemoteForward("dev", config.Route{Name: "public", SSHTarget: "devbox"}, "127.0.0.1:18765")
	if !check.OK {
		t.Fatalf("check failed: %+v", check)
	}
	if check.Name != "forward dev/public" || check.Detail != "remote can reach 127.0.0.1:18765" {
		t.Fatalf("check = %+v", check)
	}
}

func TestCheckRemoteForwardGivesReconnectGuidance(t *testing.T) {
	previous := checkForwardHealth
	t.Cleanup(func() { checkForwardHealth = previous })
	checkForwardHealth = func(target, addr string) error {
		return errors.New("connection refused")
	}

	check := checkRemoteForward("dev", config.Route{Name: "public", SSHTarget: "devbox"}, "127.0.0.1:18765")
	if check.OK {
		t.Fatalf("expected failed check: %+v", check)
	}
	for _, want := range []string{"connection refused", "reconnect SSH"} {
		if !strings.Contains(check.Detail, want) {
			t.Fatalf("detail %q missing %q", check.Detail, want)
		}
	}
}
