package terminal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testGhosttyID = "6D971530-022E-445E-BC00-081EFEDC2C5A"
const testGhosttyInstance = `{"pid":123,"started":1780000000.125}`

func TestGhosttyCapturesNamespacedAppleScriptID(t *testing.T) {
	p := GhosttyProvider{Runner: func(_ string, args ...string) ([]byte, error) {
		if args[len(args)-1] == ghosttyInstanceScript {
			return []byte(testGhosttyInstance), nil
		}
		return []byte(testGhosttyID + "\n"), nil
	}}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.Terminal != "ghostty" || !strings.HasPrefix(s.SessionKey, "ghostty:") || !strings.HasSuffix(s.SessionKey, ":"+testGhosttyID) || s.DetectedHost != "" {
		t.Fatalf("session = %+v", s)
	}
}

func TestGhosttyCaptureRejectsInvalidOrRestartedApp(t *testing.T) {
	for _, tt := range []struct {
		name, id, instance string
		restart            bool
	}{
		{name: "empty ID", instance: testGhosttyInstance},
		{name: "core ID is not AppleScript UUID", id: "a123f", instance: testGhosttyInstance},
		{name: "invalid launch metadata", id: testGhosttyID, instance: `{}`},
		{name: "invalid JSON", id: testGhosttyID, instance: `not JSON`},
		{name: "app restarted", id: testGhosttyID, instance: testGhosttyInstance, restart: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			captured := false
			p := GhosttyProvider{Runner: func(_ string, args ...string) ([]byte, error) {
				if args[len(args)-1] == ghosttyInstanceScript {
					if captured && tt.restart {
						return []byte(`{"pid":123,"started":1780000100}`), nil
					}
					return []byte(tt.instance), nil
				}
				captured = true
				return []byte(tt.id), nil
			}}
			if s, err := p.ActiveSession(); err == nil || s.SessionKey != "" {
				t.Fatalf("session=%+v err=%v", s, err)
			}
		})
	}
}

func TestGhosttyPasteUsesPrivatePayloadAndCapturedTarget(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "automation failure"}[fails], func(t *testing.T) {
			text := "quotes \" $()\\; हिन्दी\n\n"
			var payloadPath string
			p := GhosttyProvider{Runner: func(_ string, args ...string) ([]byte, error) {
				if args[len(args)-1] == ghosttyInstanceScript {
					return []byte(testGhosttyInstance), nil
				}
				if args[1] != ghosttyPasteScript || args[2] != testGhosttyID {
					t.Fatalf("paste did not address captured ID: %v", args)
				}
				payloadPath = args[3]
				info, err := os.Stat(payloadPath)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("private payload: info=%v err=%v", info, err)
				}
				data, err := os.ReadFile(payloadPath)
				if err != nil || string(data) != text {
					t.Fatalf("payload=%q err=%v", data, err)
				}
				if strings.Contains(strings.Join(args, " "), text) {
					t.Fatal("clipboard content exposed in command arguments")
				}
				if fails {
					return nil, errors.New("pane closed")
				}
				return nil, nil
			}}
			generation, err := p.instance()
			if err != nil {
				t.Fatal(err)
			}
			err = p.PasteText(Session{SessionKey: "ghostty:" + generation + ":" + testGhosttyID}, text)
			if (err != nil) != fails {
				t.Fatalf("PasteText error = %v", err)
			}
			if payloadPath == "" {
				t.Fatal("paste was not delivered")
			}
			if _, err := os.Stat(payloadPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("payload file was not removed: %v", err)
			}
		})
	}
}

func TestGhosttyPasteRefusesRestoredUUIDInNewApp(t *testing.T) {
	p := GhosttyProvider{Runner: func(_ string, args ...string) ([]byte, error) {
		if args[len(args)-1] != ghosttyInstanceScript {
			t.Fatal("attempted to paste to a restarted app")
		}
		return []byte(testGhosttyInstance), nil
	}}
	if err := p.PasteText(Session{SessionKey: "ghostty:old-generation:" + testGhosttyID}, "private"); err == nil {
		t.Fatal("accepted a stale binding")
	}
}

func TestGhosttyInstanceScriptOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS AppKit and JXA")
	}
	_, err := (GhosttyProvider{}).instance()
	if err != nil && !strings.Contains(err.Error(), "Ghostty is not running as a single app instance") {
		t.Fatal(err)
	}
}

// Compilation checks the real installed application's dictionary without
// opening a window, granting Automation permission, or sending any input.
func TestGhosttyAppleScriptCompilation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS AppleScript")
	}
	if _, err := os.Stat("/Applications/Ghostty.app"); errors.Is(err, os.ErrNotExist) {
		t.Skip("Ghostty is not installed; CI installs it for the dedicated syntax check")
	} else if err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"capture": ghosttyActiveScript, "paste": ghosttyPasteScript} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "check.applescript")
			if err := os.WriteFile(source, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("osacompile", "-o", filepath.Join(dir, "check.scpt"), source).CombinedOutput()
			if err != nil {
				t.Fatalf("Ghostty AppleScript does not compile: %v: %s\n%s", err, out, script)
			}
		})
	}
}
