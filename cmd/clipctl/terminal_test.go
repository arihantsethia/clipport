package main

import (
	"encoding/json"
	"errors"
	"github.com/arihantsethia/clipport/internal/daemon"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/terminal"
)

func TestResolveSessionKey(t *testing.T) {
	for _, tt := range []struct {
		name, pane, explicit, program, want string
		wantError                           bool
	}{
		{want: "iterm-id"},
		{explicit: "supplied", want: "supplied"},
		{name: "ghostty", want: "ghostty:app:uuid"},
		{program: "ghostty", wantError: true},
		{name: "unsupported", wantError: true},
		{pane: "3", wantError: true},
		{name: "ghostty", pane: "3", wantError: true},
		{name: "ghostty", explicit: "supplied", wantError: true},
		{name: "wezterm", pane: "3", wantError: true}, // no originating GUI socket
	} {
		env := map[string]string{"TERM_PROGRAM": tt.program, "TERM_SESSION_ID": "iterm-id"}
		got, err := resolveSessionKey(tt.name, tt.pane, tt.explicit, func(k string) string { return env[k] }, func() (terminal.Session, error) {
			if tt.name != "ghostty" {
				t.Fatal("unexpected focus lookup")
			}
			return terminal.Session{SessionKey: "ghostty:app:uuid"}, nil
		})
		if got != tt.want || (err != nil) != tt.wantError {
			t.Errorf("%+v: key=%q err=%v", tt, got, err)
		}
	}
}

func TestPasteToGhosttyStopsOnDaemonError(t *testing.T) {
	const id = "6D971530-022E-445E-BC00-081EFEDC2C5A"
	provider := terminal.GhosttyProvider{Runner: func(_ string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "-l" {
			return []byte(`{"pid":123,"started":1780000000}`), nil
		}
		if len(args) != 2 {
			t.Fatal("attempted to paste after daemon error")
		}
		return []byte(id), nil
	}}
	failure := errors.New("upload failed")
	err := pasteToGhostty(provider, func(key string) (string, error) {
		if !strings.HasPrefix(key, "ghostty:") || !strings.HasSuffix(key, ":"+id) {
			t.Fatalf("key=%q", key)
		}
		return "must not paste", failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
}

func TestRepairDoesNotBorrowAnotherFocusedPanesHost(t *testing.T) {
	repaired, err := repairUnmappedSession("unused", "origin", func() (*config.Config, error) {
		return &config.Config{Hosts: []config.Host{{Name: "prod"}}}, nil
	}, func() (terminal.Session, error) {
		return terminal.Session{SessionKey: "other", DetectedHost: "prod"}, nil
	}, func([]config.Host) (string, bool, error) {
		t.Fatal("attempted to repair a pane using unrelated focus")
		return "", false, nil
	})
	if err != nil || repaired {
		t.Fatalf("repaired=%v err=%v", repaired, err)
	}
}

func TestFetchPasteUsesBoundOnlyProtocol(t *testing.T) {
	for _, tt := range []struct {
		name      string
		response  daemon.Response
		want      string
		wantError string
	}{
		{name: "text is unchanged", response: daemon.Response{Text: "quotes \" $()\n\n"}, want: "quotes \" $()\n\n"},
		{name: "image path", response: daemon.Response{Path: "/tmp/clipport/user/image.png"}, want: "/tmp/clipport/user/image.png"},
		{name: "old daemon", response: daemon.Response{Error: "unknown command"}, wantError: "restart Clipport"},
		{name: "unmapped pane", response: daemon.Response{Error: daemon.PasteUnavailable, Debug: "failed to match terminal session"}, wantError: "failed to match terminal session"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the path below macOS's Unix-domain socket length limit.
			dir, err := os.MkdirTemp("", "clipport-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			socket := filepath.Join(dir, "daemon.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			requests := make(chan daemon.Request, 1)
			failures := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					failures <- err
					return
				}
				defer conn.Close()
				var req daemon.Request
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					failures <- err
					return
				}
				requests <- req
				failures <- json.NewEncoder(conn).Encode(tt.response)
			}()
			got, err := fetchPaste(socket, "wezterm:instance:42", true)
			if got != tt.want || (tt.wantError == "" && err != nil) || (tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError))) {
				t.Fatalf("output=%q err=%v", got, err)
			}
			if err := <-failures; err != nil {
				t.Fatal(err)
			}
			req := <-requests
			if req.Command != "paste_bound" || req.SessionKey != "wezterm:instance:42" {
				t.Fatalf("request=%+v", req)
			}
		})
	}
}
