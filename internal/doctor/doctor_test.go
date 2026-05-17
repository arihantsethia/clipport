package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
)

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
