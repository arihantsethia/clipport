package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
)

func TestCheckConfiguredRemoteForwardDistinguishesOptionalFromBroken(t *testing.T) {
	oldRead, oldCheck := readEffectiveSSHConfig, checkForwardHealth
	t.Cleanup(func() { readEffectiveSSHConfig, checkForwardHealth = oldRead, oldCheck })
	for _, tt := range []struct {
		name, effective       string
		inspectErr, healthErr error
		skip, ok, probe       bool
	}{
		{name: "not configured", skip: true, ok: true},
		{name: "unrelated forwarding", effective: "remoteforward [127.0.0.1]:9000 [127.0.0.1]:9000\n", skip: true, ok: true},
		{name: "configured healthy", effective: "remoteforward [127.0.0.1]:18765 [127.0.0.1]:18765\n", ok: true, probe: true},
		{name: "configured broken", effective: "remoteforward [127.0.0.1]:18765 [127.0.0.1]:18765\n", healthErr: errors.New("connection refused"), probe: true},
		{name: "misdirected wrong port", effective: "remoteforward [127.0.0.1]:18765 [127.0.0.1]:9000\n"},
		{name: "misdirected external host", effective: "remoteforward [127.0.0.1]:18765 [attacker.example]:80\n"},
		{name: "misdirected external same port", effective: "remoteforward [127.0.0.1]:19000 [attacker.example]:18765\n"},
		{name: "misdirected external IP", effective: "remoteforward [127.0.0.1]:18765 [198.51.100.1]:18765\n"},
		{name: "misdirected wildcard destination", effective: "remoteforward [127.0.0.1]:18765 [0.0.0.0]:18765\n"},
		{name: "misdirected duplicate rule", effective: "remoteforward [127.0.0.1]:18765 [127.0.0.1]:18765\nremoteforward [127.0.0.1]:18765 [attacker.example]:80\n"},
		{name: "inspection failed", inspectErr: errors.New("invalid SSH config")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			readEffectiveSSHConfig = func(path, target string) (string, error) {
				if path != "/custom/ssh_config" || target != "devbox" {
					t.Fatalf("path=%q target=%q", path, target)
				}
				return tt.effective, tt.inspectErr
			}
			checkForwardHealth = func(target, addr string) error { called = true; return tt.healthErr }
			check := checkConfiguredRemoteForward("dev", config.Route{Name: "public", SSHTarget: "devbox"}, "127.0.0.1:18765", "/custom/ssh_config")
			if check.OK != tt.ok || check.Skipped != tt.skip || called != tt.probe {
				t.Fatalf("check=%+v called=%v", check, called)
			}
			if strings.HasPrefix(tt.name, "misdirected") && !strings.Contains(check.Detail, "forwarding conflict") {
				t.Fatalf("missing conflict diagnostic: %+v", check)
			}
			if tt.healthErr != nil && !strings.Contains(check.Detail, tt.healthErr.Error()) {
				t.Fatalf("lost health failure: %+v", check)
			}
		})
	}
}

func TestHasClipboardForwardUsesEffectiveEndpointPorts(t *testing.T) {
	for _, tt := range []struct {
		line string
		want bool
	}{
		{"remoteforward [127.0.0.1]:18765 [127.0.0.1]:18765", true},
		{"remoteforward [localhost]:18765 [localhost]:18765", true},
		{"remoteforward [::1]:18765 [::1]:18765", true},
		{"remoteforward 18765 [localhost]:18765", true},
		{"remoteforward [127.0.0.1]:9000 [localhost]:18765", true},
		{"localforward [127.0.0.1]:18765 [localhost]:18765", false},
		{"remoteforward [127.0.0.1]:9000 [localhost]:9000", false},
	} {
		if _, got, err := clipboardForwardAddress(tt.line, "127.0.0.1:18765"); err != nil || got != tt.want {
			t.Errorf("%s: got %v, want %v, err=%v", tt.line, got, tt.want, err)
		}
	}
}

func TestClipboardForwardAddressUsesActualRemoteEndpoint(t *testing.T) {
	for _, tt := range []struct{ line, want string }{
		{"remoteforward [localhost]:19000 [127.0.0.1]:18765", "localhost:19000"},
		{"remoteforward [::1]:19000 [localhost]:18765", "[::1]:19000"},
		{"remoteforward 19000 [localhost]:18765", "localhost:19000"},
		{"remoteforward [0.0.0.0]:18765 [localhost]:18765", "127.0.0.1:18765"},
	} {
		addr, configured, err := clipboardForwardAddress(tt.line, "127.0.0.1:18765")
		if err != nil || !configured || addr != tt.want {
			t.Errorf("line=%s addr=%s configured=%v err=%v", tt.line, addr, configured, err)
		}
	}
	_, configured, err := clipboardForwardAddress("remoteforward [198.51.100.1]:18765 [localhost]:18765", "127.0.0.1:18765")
	if err == nil || !configured {
		t.Fatal("non-loopback remote endpoint should fail without transmitting token")
	}
}
