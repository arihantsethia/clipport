package terminal

import "testing"

func TestExtractHostFromItermTitle(t *testing.T) {
	title := "dev@vm-devbox: ~ (cloudflared) — DEV-MAC.local"
	if got := ExtractHost(title); got != "vm-devbox" {
		t.Fatalf("ExtractHost() = %q, want vm-devbox", got)
	}
}

func TestItermProviderUsesRunner(t *testing.T) {
	p := ItermProvider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("dev@vm-devbox: ~/projects, session-uuid\n"), nil
		},
	}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.Terminal != "iterm" || s.SessionKey != "session-uuid" || s.RawTitle != "dev@vm-devbox: ~/projects" || s.DetectedHost != "vm-devbox" || s.Kind != SessionRemote {
		t.Fatalf("session = %+v", s)
	}
}

func TestItermProviderFallsBackToTitleAsSessionKey(t *testing.T) {
	p := ItermProvider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("dev@vm-devbox: ~/projects\n"), nil
		},
	}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionKey != "dev@vm-devbox: ~/projects" || s.RawTitle != "dev@vm-devbox: ~/projects" {
		t.Fatalf("session = %+v", s)
	}
}

func TestItermProviderAllowsCommaInTitle(t *testing.T) {
	p := ItermProvider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("dev, shell, session-uuid\n"), nil
		},
	}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.RawTitle != "dev, shell" || s.SessionKey != "session-uuid" {
		t.Fatalf("session = %+v", s)
	}
}

func TestItermProviderClassifiesKnownLocalShellTitle(t *testing.T) {
	p := ItermProvider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("zsh, session-uuid\n"), nil
		},
	}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != SessionLocal || s.DetectedHost != "" {
		t.Fatalf("session = %+v", s)
	}
}

func TestItermProviderLeavesUnknownTitleUnclassified(t *testing.T) {
	p := ItermProvider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("production\n"), nil
		},
	}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.Kind != SessionUnknown {
		t.Fatalf("session = %+v", s)
	}
}

func TestNormalizeSessionKey(t *testing.T) {
	const upperID = "6D971530-022E-445E-BC00-081EFEDC2C5A"
	const lowerID = "6d971530-022e-445e-bc00-081efedc2c5a"
	for _, tt := range []struct {
		name string
		key  string
		want string
	}{
		{name: "environment key", key: "w0t1p2:" + upperID, want: upperID},
		{name: "multi-digit indexes", key: "w12t34p56:" + upperID, want: upperID},
		{name: "preserves UUID case", key: "w0t0p0:" + lowerID, want: lowerID},
		{name: "surrounding whitespace", key: "  w0t1p0:" + upperID + "\n", want: upperID},
		{name: "bare ID", key: upperID, want: upperID},
		{name: "opaque key", key: "term-session-id", want: "term-session-id"},
		{name: "title containing colon", key: "dev@host: ~/projects", want: "dev@host: ~/projects"},
		{name: "arbitrary prefix", key: "custom:" + upperID, want: "custom:" + upperID},
		{name: "invalid indexes", key: "wxt1p0:" + upperID, want: "wxt1p0:" + upperID},
		{name: "invalid UUID", key: "w0t1p0:not-a-uuid", want: "w0t1p0:not-a-uuid"},
		{name: "missing UUID", key: "w0t1p0:", want: "w0t1p0:"},
		{name: "trailing data", key: "w0t1p0:" + upperID + ":extra", want: "w0t1p0:" + upperID + ":extra"},
		{name: "empty key"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSessionKey(tt.key); got != tt.want {
				t.Fatalf("NormalizeSessionKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
