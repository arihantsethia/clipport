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
