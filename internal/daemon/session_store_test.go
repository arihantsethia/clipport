package daemon

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/terminal"
)

func TestLoadSessionBindingsNormalizesLegacyItermKeys(t *testing.T) {
	const id = "6D971530-022E-445E-BC00-081EFEDC2C5A"
	const envKey = "w0t1p0:" + id
	now := time.Now()
	for _, tt := range []struct {
		name     string
		bindings map[string]sessionBinding
		want     string
	}{
		{
			name: "environment key only",
			bindings: map[string]sessionBinding{
				envKey: {Machine: "devbox", LastUsed: now},
			},
			want: "devbox",
		},
		{
			name: "newer environment key",
			bindings: map[string]sessionBinding{
				envKey: {Machine: "devbox", LastUsed: now},
				id:     {Machine: "prod", LastUsed: now.Add(-time.Hour)},
			},
			want: "devbox",
		},
		{
			name: "newer bare ID",
			bindings: map[string]sessionBinding{
				envKey: {Machine: "devbox", LastUsed: now.Add(-time.Hour)},
				id:     {Machine: "prod", LastUsed: now},
			},
			want: "prod",
		},
		{
			name: "equal timestamps prefer bare ID",
			bindings: map[string]sessionBinding{
				envKey: {Machine: "devbox", LastUsed: now},
				id:     {Machine: "prod", LastUsed: now},
			},
			want: "prod",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			legacy := &Server{sessionStorePath: path, registered: tt.bindings}
			if err := legacy.saveSessionBindings(); err != nil {
				t.Fatal(err)
			}
			s := &Server{
				Config:           &config.Config{Hosts: []config.Host{{Name: "devbox"}, {Name: "prod"}}},
				sessionStorePath: path,
			}
			if err := s.loadSessionBindings(); err != nil {
				t.Fatal(err)
			}
			if got := s.Status().Registered; got != 1 {
				t.Fatalf("registered = %d, want 1", got)
			}
			for _, key := range []string{envKey, id} {
				host, ok := s.resolveSessionHost(terminal.Session{SessionKey: key})
				if !ok || host.Name != tt.want {
					t.Fatalf("host for %q = %+v, found = %v; want %s", key, host, ok, tt.want)
				}
			}
		})
	}
}

func TestConcurrentSessionSavesPreserveBindings(t *testing.T) {
	const workers, registrations = 8, 8
	s := &Server{
		Config:           &config.Config{Hosts: []config.Host{{Name: "devbox"}}},
		sessionStorePath: filepath.Join(t.TempDir(), "sessions.db"),
		registered:       map[string]sessionBinding{},
	}
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for n := range registrations {
				key := fmt.Sprintf("session-%d-%d", worker, n)
				resp := s.Handle(Request{Command: "register_session", SessionKey: key, Machine: "devbox"})
				if resp.Error != "" {
					errs <- fmt.Errorf("register %s: %s", key, resp.Error)
					return
				}
				// Lookup updates LastUsed while other requests persist their snapshots.
				if _, ok := s.resolveSessionHost(terminal.Session{SessionKey: key}); !ok {
					errs <- fmt.Errorf("registered session %s disappeared", key)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Do not save again here: that would hide a stale snapshot winning the
	// last write. Every successful registration must already be durable.
	reloaded := &Server{Config: s.Config, sessionStorePath: s.sessionStorePath}
	if err := reloaded.loadSessionBindings(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Status().Registered; got != workers*registrations {
		t.Fatalf("persisted bindings = %d, want %d", got, workers*registrations)
	}
	for worker := range workers {
		for n := range registrations {
			key := fmt.Sprintf("session-%d-%d", worker, n)
			host, ok := reloaded.resolveSessionHost(terminal.Session{SessionKey: key})
			if !ok || host.Name != "devbox" {
				t.Errorf("reloaded host for %s = %+v, found = %v; want devbox", key, host, ok)
			}
		}
	}
}
