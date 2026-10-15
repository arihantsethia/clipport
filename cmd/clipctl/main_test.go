package main

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
)

func TestHelpFlagPrintsCommandUsage(t *testing.T) {
	output, err := runClipctlForTest("--help")
	if err != nil {
		t.Fatalf("clipctl --help failed: %v\n%s", err, output)
	}
	assertCommandUsage(t, output)
}

func TestHelpCommandPrintsCommandUsage(t *testing.T) {
	output, err := runClipctlForTest("help")
	if err != nil {
		t.Fatalf("clipctl help failed: %v\n%s", err, output)
	}
	assertCommandUsage(t, output)
}

func TestPasteErrorMessageHidesSocketDetailsInNormalMode(t *testing.T) {
	msg := pasteErrorMessage(daemon.Response{}, errors.New("dial unix /tmp/clipport/501/clipportd.sock: connect: no such file or directory"), false)
	if msg != daemon.PasteUnavailable {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, "/tmp/clipport") || strings.Contains(msg, "dial unix") {
		t.Fatalf("message leaked diagnostic detail: %q", msg)
	}
}

func TestPasteErrorMessageShowsSocketDetailsInDebugMode(t *testing.T) {
	msg := pasteErrorMessage(daemon.Response{}, errors.New("dial unix /tmp/clipport/501/clipportd.sock: connect: no such file or directory"), true)
	if !strings.Contains(msg, daemon.PasteUnavailable) || !strings.Contains(msg, "dial unix") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPasteErrorMessageUsesDaemonDebugInDebugMode(t *testing.T) {
	resp := daemon.Response{Error: daemon.PasteUnavailable, Debug: "clipboard has no image or text"}
	msg := pasteErrorMessage(resp, errors.New(resp.Error), true)
	if !strings.Contains(msg, "clipboard has no image or text") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPasteOutputReturnsTextBeforePath(t *testing.T) {
	resp := daemon.Response{Path: "/tmp/clipport/file.txt", Text: "hello"}
	if got := pasteOutput(resp); got != "hello" {
		t.Fatalf("output = %q", got)
	}
}

func TestIsUnmappedSessionFailureDetectsMappingFailure(t *testing.T) {
	resp := daemon.Response{Debug: `failed to match active iTerm session (title "tmux"; detected host unavailable)`}
	if !isUnmappedSessionFailure(resp) {
		t.Fatal("expected mapping failure")
	}
}

func TestRepairMachineForSessionUsesDetectedHostWithoutPrompt(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{Name: "devbox", MatchHosts: []string{"devbox.local"}}}}
	called := false
	machine, ok, err := repairMachineForDetectedHost(
		"devbox.local",
		cfg,
		func([]config.Host) (string, bool, error) {
			called = true
			return "", false, nil
		},
	)
	if err != nil || !ok || machine != "devbox" {
		t.Fatalf("machine=%q ok=%v err=%v", machine, ok, err)
	}
	if called {
		t.Fatal("did not expect prompt for detected host")
	}
}

func TestRepairMachineForSessionPromptsWhenHostIsUnavailable(t *testing.T) {
	cfg := &config.Config{
		DefaultHost: "devbox",
		Hosts:       []config.Host{{Name: "devbox"}},
	}
	machine, ok, err := repairMachineForDetectedHost(
		"",
		cfg,
		func(hosts []config.Host) (string, bool, error) {
			if len(hosts) != 1 || hosts[0].Name != "devbox" {
				t.Fatalf("hosts=%+v", hosts)
			}
			return "devbox", true, nil
		},
	)
	if err != nil || !ok || machine != "devbox" {
		t.Fatalf("machine=%q ok=%v err=%v", machine, ok, err)
	}
}

func TestRepairMachineForSessionDoesNotUseDefaultHostForUnknownDetectedHost(t *testing.T) {
	cfg := &config.Config{
		DefaultHost: "devbox",
		Hosts:       []config.Host{{Name: "devbox"}},
	}
	prompted := false
	machine, ok, err := repairMachineForDetectedHost(
		"unknown",
		cfg,
		func([]config.Host) (string, bool, error) {
			prompted = true
			return "devbox", true, nil
		},
	)
	if err != nil || !ok || machine != "devbox" {
		t.Fatalf("machine=%q ok=%v err=%v", machine, ok, err)
	}
	if !prompted {
		t.Fatal("expected prompt for unknown detected host")
	}
}

func runClipctlForTest(args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func assertCommandUsage(t *testing.T, output string) {
	t.Helper()
	for _, want := range []string{
		"usage: clipctl [--socket path] [--debug] paste",
		"clipctl [--socket path] session register --machine name",
		"clipctl start [--config path]",
		"clipctl shims setup --host machine",
		"clipctl update [--config path]",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("usage output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Usage of") {
		t.Fatalf("usage output used Go flag defaults:\n%s", output)
	}
}
