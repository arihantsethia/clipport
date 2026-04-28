package terminal

import "testing"

func TestExtractHostFromItermTitle(t *testing.T) {
	title := "dev@vm-devbox: ~ (cloudflared) — CSS-MGGWV0XYQX.local"
	if got := ExtractHost(title); got != "vm-devbox" {
		t.Fatalf("ExtractHost() = %q, want vm-devbox", got)
	}
}

func TestItermProviderUsesRunner(t *testing.T) {
	p := ItermProvider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("dev@vm-devbox: ~/projects\n"), nil
		},
	}
	s, err := p.ActiveSession()
	if err != nil {
		t.Fatal(err)
	}
	if s.Terminal != "iterm" || s.DetectedHost != "vm-devbox" {
		t.Fatalf("session = %+v", s)
	}
}
