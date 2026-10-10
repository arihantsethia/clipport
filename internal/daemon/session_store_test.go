package daemon

import (
	"path/filepath"
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
