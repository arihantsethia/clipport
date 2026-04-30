package daemon

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arihantsethia/clipport/internal/clipboard"
	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/remote"
	"github.com/arihantsethia/clipport/internal/terminal"
)

type fakeSessions struct{ session terminal.Session }

func (f fakeSessions) ActiveSession() (terminal.Session, error) { return f.session, nil }

type fakeClipboard struct {
	item clipboard.Item
	err  error
}

func (f fakeClipboard) Read() (clipboard.Item, error) {
	if f.err != nil {
		return clipboard.Item{}, f.err
	}
	return f.item, nil
}

type fakePaster struct {
	called bool
	err    error
}

func (f *fakePaster) Paste() error {
	f.called = true
	return f.err
}

func TestPrepareUnixSocketKeepsLiveSocket(t *testing.T) {
	socketPath := shortSocketPath(t)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if err := prepareUnixSocket(socketPath); err == nil {
		t.Fatal("expected live socket to be preserved")
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("live socket was removed: %v", err)
	}
}

func TestPrepareUnixSocketRemovesStaleSocket(t *testing.T) {
	socketPath := shortSocketPath(t)
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := prepareUnixSocket(socketPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale socket still exists: %v", err)
	}
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "wbsock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

func TestPasteReturnsRemotePNGPath(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    remote.NewManager(func(target string) bool { return true }),
		Uploader: remote.Uploader{
			Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
			Runner: func(data []byte, target, remotePath string) error {
				if string(data) != "png" || target != "devbox-public" {
					t.Fatalf("upload args = %q %q %q", string(data), target, remotePath)
				}
				return nil
			},
		},
		registered: map[string]sessionBinding{},
	}
	resp, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resp.Path, "/tmp/clipport/") || !strings.HasSuffix(resp.Path, ".png") {
		t.Fatalf("path = %q", resp.Path)
	}
	st := s.Status()
	if len(st.Recent) != 1 {
		t.Fatalf("recent transfers = %d, want 1", len(st.Recent))
	}
	if st.Recent[0].Bytes != 3 || st.Recent[0].Route != "public" {
		t.Fatalf("recent transfer = %+v", st.Recent[0])
	}
}

func TestStatusIncludesPreferredHostRouteTarget(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name: "devbox",
		Routes: []config.Route{
			{Name: "public", SSHTarget: "devbox.example.com", Priority: 20},
			{Name: "lan", SSHTarget: "192.168.1.20", Priority: 10},
		},
	}}}
	probed := false
	s := &Server{
		Config: cfg,
		Routes: remote.NewManager(func(target string) bool {
			probed = true
			return target == "192.168.1.20"
		}),
		registered: map[string]sessionBinding{},
	}
	s.Routes.Tester = func(route config.Route) (time.Duration, bool) {
		probed = true
		return 0, false
	}

	st := s.Status()
	if len(st.Hosts) != 1 {
		t.Fatalf("hosts = %+v", st.Hosts)
	}
	if st.Hosts[0].Name != "devbox" || st.Hosts[0].Route != "lan" || st.Hosts[0].Target != "192.168.1.20" {
		t.Fatalf("host status = %+v", st.Hosts[0])
	}
	if probed {
		t.Fatal("status should not probe routes")
	}
}

func TestPasteRetriesImageUploadWithFreshRoute(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes: []config.Route{
			{Name: "public", SSHTarget: "devbox-public", Priority: 20},
			{Name: "lan", SSHTarget: "devbox-lan", Priority: 10},
		},
	}}}
	routes := remote.NewManager(func(target string) bool { return true })
	publicFirst := true
	routes.Tester = func(route config.Route) (time.Duration, bool) {
		if publicFirst {
			if route.Name == "public" {
				return 80 * time.Millisecond, true
			}
			return 250 * time.Millisecond, true
		}
		if route.Name == "lan" {
			return 60 * time.Millisecond, true
		}
		return 300 * time.Millisecond, true
	}
	attempts := 0
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    routes,
		Uploader: remote.Uploader{
			Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
			Runner: func(data []byte, target, remotePath string) error {
				attempts++
				if attempts == 1 {
					publicFirst = false
					if target != "devbox-public" {
						t.Fatalf("first target = %q, want devbox-public", target)
					}
					return errors.New("route went stale")
				}
				if target != "devbox-lan" {
					t.Fatalf("retry target = %q, want devbox-lan", target)
				}
				return nil
			},
		},
		registered: map[string]sessionBinding{},
	}

	resp, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if resp.Path == "" {
		t.Fatal("path is empty")
	}
	if st := s.Status(); st.Recent[0].Route != "lan" {
		t.Fatalf("recorded route = %q, want lan", st.Recent[0].Route)
	}
}

func TestHandlePasteReturnsRemoteTextDirectly(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	uploaded := false
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindText, Data: []byte("hello")}},
		Routes:    remote.NewManager(func(target string) bool { return true }),
		Uploader: remote.Uploader{
			Runner: func(data []byte, target, remotePath string) error {
				uploaded = true
				return nil
			},
		},
		registered: map[string]sessionBinding{},
	}
	resp := s.Handle(Request{Command: "paste", SessionKey: "s1", Host: "vm-devbox"})
	if resp.Error != "" {
		t.Fatalf("error = %q", resp.Error)
	}
	if resp.Path != "" || resp.Text != "hello" {
		t.Fatalf("resp = %+v", resp)
	}
	if uploaded {
		t.Fatal("text paste uploaded a file")
	}
}

func TestHandlePasteReturnsClipboardError(t *testing.T) {
	s := &Server{
		Config: &config.Config{Hosts: []config.Host{{
			Name:       "devbox",
			MatchHosts: []string{"vm-devbox"},
			Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
		}}},
		Clipboard:  fakeClipboard{err: errors.New("clipboard has no image or text")},
		Paster:     nil,
		Routes:     remote.NewManager(func(target string) bool { return true }),
		registered: map[string]sessionBinding{},
	}
	resp := s.Handle(Request{Command: "paste", SessionKey: "s1", Host: "vm-devbox"})
	if resp.Error != PasteUnavailable {
		t.Fatalf("error = %q", resp.Error)
	}
	if !strings.Contains(resp.Debug, "clipboard has no image or text") {
		t.Fatalf("debug = %q", resp.Debug)
	}
}

func TestExplicitLocalSessionInvokesNativePaste(t *testing.T) {
	paster := &fakePaster{}
	s := &Server{
		Config:     &config.Config{},
		Paster:     paster,
		Routes:     remote.NewManager(func(target string) bool { return true }),
		registered: map[string]sessionBinding{},
	}
	resp, err := s.Paste(terminal.Session{SessionKey: "s1", Kind: terminal.SessionLocal, RawTitle: "zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if !paster.called {
		t.Fatal("native paste was not called")
	}
	if resp.Path != "" || resp.Text != "" {
		t.Fatalf("resp = %+v, want empty for local paste", resp)
	}
}

func TestEmptyDetectedHostDoesNotResolveDefaultHost(t *testing.T) {
	paster := &fakePaster{}
	s := &Server{
		Config: &config.Config{
			DefaultHost: "devbox",
			Hosts: []config.Host{{
				Name:   "devbox",
				Routes: []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
			}},
		},
		Sessions:    fakeSessions{terminal.Session{SessionKey: "s1", Kind: terminal.SessionLocal, RawTitle: "zsh"}},
		Clipboard:   fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Paster:      paster,
		Routes:      remote.NewManager(func(target string) bool { return true }),
		registered:  map[string]sessionBinding{},
		recent:      nil,
		recentBinds: nil,
	}

	resp, err := s.Paste(terminal.Session{SessionKey: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if !paster.called {
		t.Fatal("native paste was not called")
	}
	if resp.Path != "" || resp.Text != "" {
		t.Fatalf("resp = %+v, want empty for local paste", resp)
	}
}

func TestUnmatchedRemoteLookingSessionDoesNotInvokeNativePaste(t *testing.T) {
	paster := &fakePaster{}
	s := &Server{
		Config:     &config.Config{},
		Paster:     paster,
		Routes:     remote.NewManager(func(target string) bool { return true }),
		registered: map[string]sessionBinding{},
	}
	_, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "mystery-box", RawTitle: "ssh dev@mystery-box", Kind: terminal.SessionRemote})
	if err == nil {
		t.Fatal("expected session match error")
	}
	if paster.called {
		t.Fatal("native paste was called for unresolved remote-looking session")
	}
	if !strings.Contains(err.Error(), "clipctl session register --machine <name>") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestLocalPasteFailureReturnsHotkeySafeErrorAndDebug(t *testing.T) {
	s := &Server{
		Config:     &config.Config{},
		Paster:     &fakePaster{err: errors.New("system events denied")},
		Routes:     remote.NewManager(func(target string) bool { return true }),
		registered: map[string]sessionBinding{},
	}
	_, err := s.Paste(terminal.Session{SessionKey: "s1", Kind: terminal.SessionLocal, RawTitle: "zsh"})
	if err == nil {
		t.Fatal("expected local paste error")
	}
	if err.Error() != "local paste failed: system events denied" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRegisterSessionUsesExplicitMachineBinding(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{
		{
			Name:       "devbox",
			MatchHosts: []string{"vm-devbox"},
			Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
		},
		{
			Name:       "prod",
			MatchHosts: []string{"vm-prod"},
			Routes:     []config.Route{{Name: "public", SSHTarget: "prod-public"}},
		},
	}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s2", DetectedHost: "vm-prod", RawTitle: "ssh dev@vm-prod"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    remote.NewManager(func(target string) bool { return true }),
		Uploader: remote.Uploader{
			Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
			Runner: func(data []byte, target, remotePath string) error {
				if target != "devbox-public" {
					t.Fatalf("target = %q, want devbox-public", target)
				}
				return nil
			},
		},
		registered: map[string]sessionBinding{},
	}

	resp := s.Handle(Request{
		Command:    "register_session",
		Machine:    "devbox",
		SessionKey: "s1",
		SSHAlias:   "devbox-public",
		SSHHost:    "devbox-public.example.com",
		SSHPort:    "22",
		SSHUser:    "dev",
	})
	if resp.Error != "" {
		t.Fatalf("register response error = %q", resp.Error)
	}

	if _, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "vm-prod", RawTitle: "ssh dev@vm-prod"}); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.Registered != 1 {
		t.Fatalf("registered = %d, want 1", st.Registered)
	}
	if len(st.RecentBindings) != 1 {
		t.Fatalf("recent bindings = %d, want 1", len(st.RecentBindings))
	}
	if st.RecentBindings[0].Machine != "devbox" || st.RecentBindings[0].SSHAlias != "devbox-public" {
		t.Fatalf("binding = %+v", st.RecentBindings[0])
	}
}

func TestPasteFallsBackToFocusedItermTitleWhenSessionKeyUnbound(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "ssh dev@vm-devbox", DetectedHost: "vm-devbox", RawTitle: "ssh dev@vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    remote.NewManager(func(target string) bool { return true }),
		Uploader: remote.Uploader{
			Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
			Runner: func(data []byte, target, remotePath string) error {
				if target != "devbox-public" {
					t.Fatalf("target = %q, want devbox-public", target)
				}
				return nil
			},
		},
		registered: map[string]sessionBinding{},
	}

	if _, err := s.Paste(terminal.Session{SessionKey: "term-session-id"}); err != nil {
		t.Fatal(err)
	}
}

func TestPasteUnmatchedSessionReportsSessionGuidance(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{
		{Name: "devbox", Routes: []config.Route{{Name: "public", SSHTarget: "devbox-public"}}},
		{Name: "prod", Routes: []config.Route{{Name: "public", SSHTarget: "prod-public"}}},
	}}
	s := &Server{
		Config:      cfg,
		Sessions:    fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "mystery-box", RawTitle: "ssh dev@mystery-box"}},
		Clipboard:   fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Paster:      &fakePaster{err: errors.New("system events denied")},
		Routes:      remote.NewManager(func(target string) bool { return true }),
		registered:  map[string]sessionBinding{},
		recent:      nil,
		recentBinds: nil,
	}

	_, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "mystery-box", RawTitle: "ssh dev@mystery-box"})
	if err == nil {
		t.Fatal("expected paste error")
	}
	for _, want := range []string{
		`detected host "mystery-box"`,
		`configured machines: devbox, prod`,
		`clipctl session register --machine <name>`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestPasteIgnoresStaleSessionBinding(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    remote.NewManager(func(target string) bool { return true }),
		Uploader: remote.Uploader{
			Now: func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
			Runner: func(data []byte, target, remotePath string) error {
				if target != "devbox-public" {
					t.Fatalf("target = %q, want devbox-public", target)
				}
				return nil
			},
		},
		registered: map[string]sessionBinding{
			"s1": {Machine: "deleted-machine"},
		},
	}
	if _, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}); err != nil {
		t.Fatal(err)
	}
}
