package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/remote"
	"github.com/arihantsethia/clipport/internal/terminal"
)

type fakeSessions struct{ session terminal.Session }

func (f fakeSessions) ActiveSession() (terminal.Session, error) { return f.session, nil }

type fakeImages struct{ data []byte }

func (f fakeImages) ReadPNG() ([]byte, error) { return f.data, nil }

func TestPasteImageReturnsRemotePath(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:   cfg,
		Sessions: fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Images:   fakeImages{[]byte("png")},
		Routes:   remote.NewManager(func(target string) bool { return true }),
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
	path, err := s.PasteImage(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "/tmp/clipport/") {
		t.Fatalf("path = %q", path)
	}
	st := s.Status()
	if len(st.Recent) != 1 {
		t.Fatalf("recent transfers = %d, want 1", len(st.Recent))
	}
	if st.Recent[0].Bytes != 3 || st.Recent[0].Route != "public" {
		t.Fatalf("recent transfer = %+v", st.Recent[0])
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
		Config:   cfg,
		Sessions: fakeSessions{terminal.Session{SessionKey: "s2", DetectedHost: "vm-prod", RawTitle: "ssh dev@vm-prod"}},
		Images:   fakeImages{[]byte("png")},
		Routes:   remote.NewManager(func(target string) bool { return true }),
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

	if _, err := s.PasteImage(terminal.Session{SessionKey: "s1", DetectedHost: "vm-prod", RawTitle: "ssh dev@vm-prod"}); err != nil {
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

func TestPasteImageFallsBackToFocusedItermTitleWhenSessionKeyUnbound(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:   cfg,
		Sessions: fakeSessions{terminal.Session{SessionKey: "ssh dev@vm-devbox", DetectedHost: "vm-devbox", RawTitle: "ssh dev@vm-devbox"}},
		Images:   fakeImages{[]byte("png")},
		Routes:   remote.NewManager(func(target string) bool { return true }),
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

	if _, err := s.PasteImage(terminal.Session{SessionKey: "term-session-id"}); err != nil {
		t.Fatal(err)
	}
}

func TestPasteImageErrorIncludesFallbackGuidance(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{
		{Name: "devbox", Routes: []config.Route{{Name: "public", SSHTarget: "devbox-public"}}},
		{Name: "prod", Routes: []config.Route{{Name: "public", SSHTarget: "prod-public"}}},
	}}
	s := &Server{
		Config:      cfg,
		Sessions:    fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "mystery-box", RawTitle: "ssh dev@mystery-box"}},
		Images:      fakeImages{[]byte("png")},
		Routes:      remote.NewManager(func(target string) bool { return true }),
		registered:  map[string]sessionBinding{},
		recent:      nil,
		recentBinds: nil,
	}

	_, err := s.PasteImage(terminal.Session{SessionKey: "s1", DetectedHost: "mystery-box", RawTitle: "ssh dev@mystery-box"})
	if err == nil {
		t.Fatal("expected paste error")
	}
	msg := err.Error()
	for _, want := range []string{
		`title "ssh dev@mystery-box"`,
		`detected host "mystery-box"`,
		`configured machines: devbox, prod`,
		`clipport session register --machine <name>`,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestInvalidBindingFallsBackToDetectedHost(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{
		Name:       "devbox",
		MatchHosts: []string{"vm-devbox"},
		Routes:     []config.Route{{Name: "public", SSHTarget: "devbox-public"}},
	}}}
	s := &Server{
		Config:   cfg,
		Sessions: fakeSessions{terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}},
		Images:   fakeImages{[]byte("png")},
		Routes:   remote.NewManager(func(target string) bool { return true }),
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
	if _, err := s.PasteImage(terminal.Session{SessionKey: "s1", DetectedHost: "vm-devbox"}); err != nil {
		t.Fatal(err)
	}
}
