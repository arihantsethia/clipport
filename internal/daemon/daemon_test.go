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
	"github.com/arihantsethia/clipport/internal/eventlog"
	"github.com/arihantsethia/clipport/internal/remote"
	"github.com/arihantsethia/clipport/internal/terminal"
)

type fakeSessions struct{ session terminal.Session }

func (f fakeSessions) ActiveSession() (terminal.Session, error) { return f.session, nil }

func fakeRoutes(probe remote.ProbeFunc) *remote.Manager {
	routes := remote.NewManager(probe)
	// Mock both probes: mocking reachability alone still runs a real SSH upload.
	routes.Tester = func(config.Route) (time.Duration, bool) { return 0, false }
	return routes
}

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

type memoryEvents struct {
	events []eventlog.Event
}

func (m *memoryEvents) Record(e eventlog.Event) error {
	m.events = append(m.events, e)
	return nil
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
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    fakeRoutes(func(target string) bool { return true }),
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

func TestPasteRecordsSuccessEvent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	events := &memoryEvents{}
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Events:    events,
		Routes:    fakeRoutes(func(target string) bool { return true }),
		Uploader: remote.Uploader{
			Now:    func() time.Time { return time.Date(2026, 4, 28, 12, 15, 41, 0, time.UTC) },
			Runner: func(data []byte, target, remotePath string) error { return nil },
		},
		registered: map[string]sessionBinding{},
	}

	if _, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}); err != nil {
		t.Fatal(err)
	}
	if len(events.events) != 1 {
		t.Fatalf("events = %d, want 1", len(events.events))
	}
	event := events.events[0]
	if !event.OK || event.Op != "paste" || event.Host != "devbox" || event.Route != "public" || event.Target != "devbox-public" || event.Kind != "png" || event.Bytes != 3 || event.Path == "" {
		t.Fatalf("event = %+v", event)
	}
}

func TestPasteRecordsFailureEvent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	events := &memoryEvents{}
	s := &Server{
		Config: &config.Config{Hosts: []config.Host{{
			Name:       "devbox",
			MatchHosts: []string{"vm-devbox"},
			Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
		}}},
		Clipboard:  fakeClipboard{err: errors.New("clipboard empty")},
		Events:     events,
		Routes:     fakeRoutes(func(target string) bool { return true }),
		registered: map[string]sessionBinding{},
	}

	_, err := s.Paste(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(events.events) != 1 {
		t.Fatalf("events = %d, want 1", len(events.events))
	}
	event := events.events[0]
	if event.OK || event.Error == "" || event.Host != "devbox" {
		t.Fatalf("event = %+v", event)
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
		Routes: fakeRoutes(func(target string) bool {
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
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes: []config.Route{
			{Name: "public", SSHTarget: "devbox-public", Priority: 20},
			{Name: "lan", SSHTarget: "devbox-lan", Priority: 10},
		},
	}}}
	routes := fakeRoutes(func(target string) bool { return true })
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
	t.Setenv("HOME", t.TempDir())
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
		Routes:    fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		Config: &config.Config{Hosts: []config.Host{{
			Name:       "devbox",
			MatchHosts: []string{"vm-devbox"},
			Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
		}}},
		Clipboard:  fakeClipboard{err: errors.New("clipboard has no image or text")},
		Paster:     nil,
		Routes:     fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
	paster := &fakePaster{}
	s := &Server{
		Config:     &config.Config{},
		Paster:     paster,
		Routes:     fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
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
		Routes:      fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
	paster := &fakePaster{}
	s := &Server{
		Config:     &config.Config{},
		Paster:     paster,
		Routes:     fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		Config:     &config.Config{},
		Paster:     &fakePaster{err: errors.New("system events denied")},
		Routes:     fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
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
		Routes:    fakeRoutes(func(target string) bool { return true }),
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

func TestRegisterSessionDoesNotBindUnrelatedActiveSession(t *testing.T) {
	for _, tt := range []struct {
		name  string
		bound bool
	}{
		{name: "unbound focused tab"},
		{name: "already bound focused tab", bound: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			active := terminal.Session{SessionKey: "other-session"}
			s := &Server{
				Config:     &config.Config{Hosts: []config.Host{{Name: "devbox"}, {Name: "prod"}}},
				Sessions:   fakeSessions{active},
				registered: map[string]sessionBinding{},
			}
			if tt.bound {
				s.registerSession(active, sessionBinding{Machine: "prod"})
			}
			resp := s.Handle(Request{Command: "register_session", Machine: "devbox", SessionKey: "origin-session"})
			if resp.Error != "" {
				t.Fatal(resp.Error)
			}
			host, ok := s.resolveSessionHost(terminal.Session{SessionKey: "origin-session"})
			if !ok || host.Name != "devbox" {
				t.Fatalf("origin host = %+v, found = %v; want devbox", host, ok)
			}
			host, ok = s.resolveSessionHost(active)
			if ok != tt.bound {
				t.Fatalf("focused tab bound = %v, want %v", ok, tt.bound)
			}
			if tt.bound && host.Name != "prod" {
				t.Fatalf("focused tab host = %q, want prod", host.Name)
			}
		})
	}
}

func TestRegisterSessionUsesOneItermBinding(t *testing.T) {
	const id = "6D971530-022E-445E-BC00-081EFEDC2C5A"
	s := &Server{
		Config:     &config.Config{Hosts: []config.Host{{Name: "devbox"}, {Name: "prod"}}},
		registered: map[string]sessionBinding{},
	}
	// Register through the shell, then reconnect through the AppleScript ID.
	// Neither path needs a focused-session provider.
	for _, tt := range []struct{ key, machine string }{
		{key: "  w0t1p0:" + id + "  ", machine: "devbox"},
		{key: id, machine: "prod"},
		{key: "w2t3p1:" + id, machine: "devbox"},
	} {
		resp := s.Handle(Request{Command: "register_session", SessionKey: tt.key, Machine: tt.machine})
		if resp.Error != "" {
			t.Fatal(resp.Error)
		}
		for _, key := range []string{id, "w0t1p0:" + id, "w2t3p1:" + id} {
			host, ok := s.resolveSessionHost(terminal.Session{SessionKey: key})
			if !ok || host.Name != tt.machine {
				t.Fatalf("host for %q = %+v, found = %v; want %s", key, host, ok, tt.machine)
			}
		}
		st := s.Status()
		if st.Registered != 1 || len(st.RecentBindings) != 1 {
			t.Fatalf("want one session binding, got %+v", st)
		}
		if st.RecentBindings[0].SessionKey != id || st.RecentBindings[0].Machine != tt.machine {
			t.Fatalf("recent binding = %+v", st.RecentBindings[0])
		}
	}
}

func TestRegisterSessionWithoutExplicitKeyUsesActiveSession(t *testing.T) {
	for _, tt := range []struct{ name, key string }{
		{name: "empty"},
		{name: "whitespace", key: "   "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{
				Config:     &config.Config{Hosts: []config.Host{{Name: "devbox"}}},
				Sessions:   fakeSessions{terminal.Session{SessionKey: "active-session"}},
				registered: map[string]sessionBinding{},
			}
			resp := s.Handle(Request{Command: "register_session", Machine: "devbox", SessionKey: tt.key})
			if resp.Error != "" {
				t.Fatal(resp.Error)
			}
			host, ok := s.resolveSessionHost(terminal.Session{SessionKey: "active-session"})
			if !ok || host.Name != "devbox" {
				t.Fatalf("active host = %+v, found = %v; want devbox", host, ok)
			}
		})
	}
}

func TestSessionBindingSurvivesDaemonRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const sessionID = "6D971530-022E-445E-BC00-081EFEDC2C5A"
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
	storePath := filepath.Join(t.TempDir(), "sessions.db")
	first := &Server{
		Config:           cfg,
		Sessions:         fakeSessions{terminal.Session{SessionKey: "unrelated-active-session", RawTitle: "tmux", Kind: terminal.SessionLocal}},
		sessionStorePath: storePath,
		registered:       map[string]sessionBinding{},
	}
	resp := first.Handle(Request{Command: "register_session", Machine: "devbox", SessionKey: "w0t1p0:" + sessionID})
	if resp.Error != "" {
		t.Fatalf("register response error = %q", resp.Error)
	}

	second := &Server{
		Config:           cfg,
		Clipboard:        fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:           fakeRoutes(func(target string) bool { return true }),
		sessionStorePath: storePath,
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
	if err := second.loadSessionBindings(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Paste(terminal.Session{SessionKey: "w0t1p0:" + sessionID, DetectedHost: "vm-prod"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := second.registered["unrelated-active-session"]; ok {
		t.Fatal("unrelated active session binding survived restart")
	}
	if _, err := second.Paste(terminal.Session{SessionKey: sessionID, RawTitle: "tmux", Kind: terminal.SessionLocal}); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterSessionReplacesReusedTerminalSession(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{
		{Name: "devbox", Routes: []config.Route{{Name: "public", SSHTarget: "devbox-public"}}},
		{Name: "prod", Routes: []config.Route{{Name: "public", SSHTarget: "prod-public"}}},
	}}
	storePath := filepath.Join(t.TempDir(), "sessions.db")
	s := &Server{
		Config:           cfg,
		sessionStorePath: storePath,
		registered:       map[string]sessionBinding{},
	}
	for _, machine := range []string{"devbox", "prod"} {
		resp := s.Handle(Request{Command: "register_session", Machine: machine, SessionKey: "same-iterm-session"})
		if resp.Error != "" {
			t.Fatalf("register %s response error = %q", machine, resp.Error)
		}
	}
	st := s.Status()
	if st.Registered != 1 {
		t.Fatalf("registered = %d, want 1", st.Registered)
	}
	if len(st.RecentBindings) != 1 || st.RecentBindings[0].Machine != "prod" {
		t.Fatalf("recent bindings = %+v", st.RecentBindings)
	}

	reloaded := &Server{Config: cfg, sessionStorePath: storePath, registered: map[string]sessionBinding{}}
	if err := reloaded.loadSessionBindings(); err != nil {
		t.Fatal(err)
	}
	host, ok := reloaded.resolveSessionHost(terminal.Session{SessionKey: "same-iterm-session"})
	if !ok || host.Name != "prod" {
		t.Fatalf("host = %+v ok=%v, want prod", host, ok)
	}
}

func TestSessionStorePrunesUnusedBindings(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{
		{Name: "devbox", Routes: []config.Route{{Name: "public", SSHTarget: "devbox-public"}}},
		{Name: "prod", Routes: []config.Route{{Name: "public", SSHTarget: "prod-public"}}},
	}}
	now := time.Now()
	storePath := filepath.Join(t.TempDir(), "sessions.db")
	s := &Server{
		Config:           cfg,
		sessionStorePath: storePath,
		registered: map[string]sessionBinding{
			"old": {
				Machine:   "devbox",
				CreatedAt: now.Add(-45 * 24 * time.Hour),
				LastUsed:  now.Add(-31 * 24 * time.Hour),
			},
			"recent": {
				Machine:   "prod",
				CreatedAt: now.Add(-45 * 24 * time.Hour),
				LastUsed:  now.Add(-2 * time.Hour),
			},
		},
	}
	if err := s.saveSessionBindings(); err != nil {
		t.Fatal(err)
	}

	reloaded := &Server{Config: cfg, sessionStorePath: storePath, registered: map[string]sessionBinding{}}
	if err := reloaded.loadSessionBindings(); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.registered["old"]; ok {
		t.Fatalf("old binding was not pruned: %+v", reloaded.registered)
	}
	if host, ok := reloaded.resolveSessionHost(terminal.Session{SessionKey: "recent"}); !ok || host.Name != "prod" {
		t.Fatalf("recent host = %+v ok=%v, want prod", host, ok)
	}
}

func TestPasteUsesFocusedItermTitleForSameUnboundSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "term-session-id", DetectedHost: "vm-devbox", RawTitle: "ssh dev@vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Hosts: []config.Host{
		{Name: "devbox", Routes: []config.Route{{Name: "public", SSHTarget: "devbox-public"}}},
		{Name: "prod", Routes: []config.Route{{Name: "public", SSHTarget: "prod-public"}}},
	}}
	s := &Server{
		Config:      cfg,
		Sessions:    fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "mystery-box", RawTitle: "ssh dev@mystery-box"}},
		Clipboard:   fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Paster:      &fakePaster{err: errors.New("system events denied")},
		Routes:      fakeRoutes(func(target string) bool { return true }),
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
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:    cfg,
		Sessions:  fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Clipboard: fakeClipboard{item: clipboard.Item{Kind: clipboard.KindPNG, Data: []byte("png")}},
		Routes:    fakeRoutes(func(target string) bool { return true }),
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

func TestExplicitTerminalPasteNeverFallsBackToAnotherPane(t *testing.T) {
	for _, key := range []string{"ghostty:launch:uuid", "wezterm:socket:0", "unbound-iterm-pane"} {
		t.Run(key, func(t *testing.T) {
			paster := &fakePaster{}
			s := &Server{
				Config: &config.Config{Hosts: []config.Host{{Name: "prod"}}},
				Sessions: fakeSessions{terminal.Session{
					SessionKey: "other-pane", DetectedHost: "prod", Kind: terminal.SessionLocal,
				}},
				Paster:     paster,
				registered: map[string]sessionBinding{},
			}
			resp := s.Handle(Request{Command: "paste", SessionKey: key})
			if resp.Error == "" || resp.Path != "" || resp.Text != "" || paster.called {
				t.Fatalf("unmapped pane used another target: resp=%+v nativePaste=%v", resp, paster.called)
			}
		})
	}
}

func TestTerminalBindingsKeepIdenticalPaneIDsSeparate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		Config:     &config.Config{Hosts: []config.Host{{Name: "dev"}, {Name: "prod"}}},
		registered: map[string]sessionBinding{},
	}
	bindings := map[string]string{
		"ghostty:app:0": "dev", "wezterm:socket:0": "prod", "wezterm:new-socket:0": "dev",
	}
	for key, machine := range bindings {
		resp := s.Handle(Request{Command: "register_session", SessionKey: key, Machine: machine})
		if resp.Error != "" {
			t.Fatal(resp.Error)
		}
	}
	for key, machine := range bindings {
		host, ok := s.resolveSessionHost(terminal.Session{SessionKey: key})
		if !ok || host.Name != machine {
			t.Fatalf("%s resolved to %+v, found=%v", key, host, ok)
		}
	}
}

func TestBoundPasteRejectsMissingBindingAndHostHints(t *testing.T) {
	for _, key := range []string{"", "same-pane", "ghostty:app:uuid", "wezterm:socket:1"} {
		paster := &fakePaster{}
		s := &Server{
			Config:     &config.Config{Hosts: []config.Host{{Name: "prod"}}},
			Sessions:   fakeSessions{terminal.Session{SessionKey: key, DetectedHost: "prod", Kind: terminal.SessionLocal}},
			Paster:     paster,
			registered: map[string]sessionBinding{},
		}
		resp := s.Handle(Request{Command: "paste_bound", SessionKey: key, Host: "prod"})
		if resp.Error == "" || resp.Text != "" || resp.Path != "" || paster.called {
			t.Fatalf("key=%q resp=%+v nativePaste=%v", key, resp, paster.called)
		}
	}
}

func TestBoundPasteReturnsRegisteredClipboardText(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := &Server{
		Config:     &config.Config{Hosts: []config.Host{{Name: "dev"}}},
		Clipboard:  fakeClipboard{item: clipboard.Item{Kind: clipboard.KindText, Data: []byte("exact text\n\n")}},
		registered: map[string]sessionBinding{"wezterm:socket:0": {Machine: "dev"}},
	}
	resp := s.Handle(Request{Command: "paste_bound", SessionKey: "wezterm:socket:0"})
	if resp.Error != "" || resp.Text != "exact text\n\n" || resp.Path != "" {
		t.Fatalf("resp=%+v", resp)
	}
}
