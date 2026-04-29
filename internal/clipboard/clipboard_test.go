package clipboard

import (
	"errors"
	"slices"
	"testing"
)

func TestReadSelectsPNGFromDetectedTypes(t *testing.T) {
	p := Provider{
		PngpastePath: "/bin/pngpaste",
		Runner: func(name string, args ...string) ([]byte, error) {
			switch name {
			case "osascript":
				return []byte("PNGf, 12, Unicode text, 5"), nil
			case "/bin/pngpaste":
				if len(args) != 1 || args[0] != "-" {
					t.Fatalf("pngpaste args = %v", args)
				}
				return []byte("png"), nil
			default:
				t.Fatalf("unexpected command %q", name)
				return nil, nil
			}
		},
	}
	item, err := p.Read()
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindPNG || string(item.Data) != "png" {
		t.Fatalf("item = %+v", item)
	}
}

func TestReadSelectsTextFromDetectedTypes(t *testing.T) {
	p := Provider{
		Runner: func(name string, args ...string) ([]byte, error) {
			switch name {
			case "osascript":
				return []byte("Unicode text, 11"), nil
			case "pbpaste":
				return []byte("hello world"), nil
			default:
				t.Fatalf("unexpected command %q", name)
				return nil, nil
			}
		},
	}
	item, err := p.Read()
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != KindText || string(item.Data) != "hello world" {
		t.Fatalf("item = %+v", item)
	}
}

func TestReadReportsUnsupportedClipboard(t *testing.T) {
	p := Provider{
		Runner: func(name string, args ...string) ([]byte, error) {
			return []byte("URL, 20"), nil
		},
	}
	_, err := p.Read()
	if err == nil || err.Error() != "clipboard has no image or text (types: URL, 20)" {
		t.Fatalf("err = %v", err)
	}
}

func TestReadReportsClipboardChangeAfterDetection(t *testing.T) {
	p := Provider{
		Runner: func(name string, args ...string) ([]byte, error) {
			switch name {
			case "osascript":
				return []byte("Unicode text, 4"), nil
			case "pbpaste":
				return nil, errors.New("clipboard changed")
			default:
				return nil, nil
			}
		},
	}
	_, err := p.Read()
	if err == nil || err.Error() != "read text clipboard failed: clipboard changed" {
		t.Fatalf("err = %v", err)
	}
}

func TestParseClipboardInfo(t *testing.T) {
	got := ParseClipboardInfo("PNGf, 12, Unicode text, 5")
	want := []string{"PNGf", "Unicode text"}
	if !slices.Equal(got, want) {
		t.Fatalf("types = %v, want %v", got, want)
	}
}
