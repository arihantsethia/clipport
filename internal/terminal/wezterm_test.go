package terminal

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWeztermSessionKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	key, err := WeztermSessionKey(path, "0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "wezterm:") || !strings.HasSuffix(key, ":0") {
		t.Fatalf("key = %q", key)
	}
	if same, err := WeztermSessionKey(path, "0"); err != nil || same != key {
		t.Fatalf("same pane: key=%q err=%v", same, err)
	}
	if other, err := WeztermSessionKey(path, "1"); err != nil || other == key {
		t.Fatalf("other pane: key=%q err=%v", other, err)
	}
	// A recreated socket at the same path must not reuse a persisted binding.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if next, err := WeztermSessionKey(path, "0"); err != nil || next == key {
		t.Fatalf("new generation: key=%q err=%v", next, err)
	}
}

func TestWeztermSessionKeyRejectsInvalidOrigin(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ path, pane string }{
		{"", "1"}, {"relative", "1"}, {file, "1"}, {file + ".missing", "1"},
		{file, ""}, {file, "-1"}, {file, "01"}, {file, "1:2"}, {file, "18446744073709551616"},
	} {
		if key, err := WeztermSessionKey(tt.path, tt.pane); err == nil || key != "" {
			t.Errorf("WeztermSessionKey(%q, %q) = %q, %v", tt.path, tt.pane, key, err)
		}
	}
}

func TestSessionKeyFromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gui.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	want, err := WeztermSessionKey(path, "12")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, program, want string
		wantError           bool
	}{
		{"iterm", "iTerm.app", "iterm-id", false},
		{"legacy iterm", "", "iterm-id", false},
		{"wezterm overrides inherited iterm key", "WezTerm", want, false},
		{"ghostty refuses unrelated environment IDs", "ghostty", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"TERM_PROGRAM": tt.program, "TERM_SESSION_ID": "iterm-id", "WEZTERM_UNIX_SOCKET": path, "WEZTERM_PANE": "12", "GHOSTTY_SURFACE_ID": "123"}
			got, err := SessionKeyFromEnv(func(k string) string { return env[k] })
			if got != tt.want || (err != nil) != tt.wantError {
				t.Fatalf("key=%q err=%v; want %q, error=%v", got, err, tt.want, tt.wantError)
			}
		})
	}
}
