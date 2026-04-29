package token

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTightensExistingTokenMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	value, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if value != "secret" {
		t.Fatalf("value = %q", value)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}
