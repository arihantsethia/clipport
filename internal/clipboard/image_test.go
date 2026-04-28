package clipboard

import (
	"errors"
	"testing"
)

func TestReadPNGUsesStdout(t *testing.T) {
	p := ImageProvider{
		PngpastePath: "/bin/pngpaste",
		Runner: func(name string, args ...string) ([]byte, error) {
			if name != "/bin/pngpaste" {
				t.Fatalf("runner name = %q", name)
			}
			if len(args) != 1 || args[0] != "-" {
				t.Fatalf("runner args = %v", args)
			}
			return []byte("png"), nil
		},
	}
	data, err := p.ReadPNG()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png" {
		t.Fatalf("data = %q, want png", data)
	}
}

func TestReadPNGReportsEmptyClipboard(t *testing.T) {
	p := ImageProvider{
		PngpastePath: "/bin/pngpaste",
		Runner: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("empty")
		},
	}
	_, err := p.ReadPNG()
	if err == nil || err.Error() != "no image found on the macOS clipboard" {
		t.Fatalf("err = %v", err)
	}
}
