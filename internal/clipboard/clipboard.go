package clipboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Kind string

const (
	KindPNG  Kind = "png"
	KindText Kind = "text"
)

type Item struct {
	Kind Kind
	Data []byte
}

type Provider struct {
	PngpastePath string
	Runner       func(name string, args ...string) ([]byte, error)
}

func (p Provider) Read() (Item, error) {
	types, raw, err := p.detectTypes()
	if err != nil {
		return Item{}, err
	}
	kind, ok := SelectKind(types)
	if !ok {
		return Item{}, fmt.Errorf("clipboard has no image or text (types: %s)", strings.TrimSpace(raw))
	}
	data, err := p.readKind(kind)
	if err != nil {
		return Item{}, err
	}
	if len(data) == 0 {
		return Item{}, fmt.Errorf("clipboard %s data was empty", kind)
	}
	return Item{Kind: kind, Data: data}, nil
}

func (p Provider) detectTypes() ([]string, string, error) {
	out, err := p.run("osascript", "-e", "clipboard info")
	if err != nil {
		return nil, "", fmt.Errorf("clipboard type detection failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return ParseClipboardInfo(string(out)), string(out), nil
}

func (p Provider) readKind(kind Kind) ([]byte, error) {
	switch kind {
	case KindPNG:
		pngpaste, err := p.findPngpaste()
		if err != nil {
			return nil, err
		}
		data, err := p.run(pngpaste, "-")
		if err != nil {
			return nil, fmt.Errorf("read PNG clipboard failed: %w", err)
		}
		return data, nil
	case KindText:
		data, err := p.run("pbpaste")
		if err != nil {
			return nil, fmt.Errorf("read text clipboard failed: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported clipboard kind %q", kind)
	}
}

func (p Provider) run(name string, args ...string) ([]byte, error) {
	if p.Runner != nil {
		return p.Runner(name, args...)
	}
	return exec.Command(name, args...).Output()
}

func ParseClipboardInfo(info string) []string {
	fields := strings.Split(info, ",")
	types := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i += 2 {
		t := strings.TrimSpace(fields[i])
		if t != "" {
			types = append(types, t)
		}
	}
	return types
}

func SelectKind(types []string) (Kind, bool) {
	for _, t := range types {
		normalized := strings.ToLower(t)
		if strings.Contains(normalized, "png") ||
			strings.Contains(normalized, "tiff") ||
			strings.Contains(normalized, "jpeg") ||
			strings.Contains(normalized, "picture") ||
			strings.Contains(normalized, "image") {
			return KindPNG, true
		}
	}
	for _, t := range types {
		normalized := strings.ToLower(t)
		if strings.Contains(normalized, "text") ||
			strings.Contains(normalized, "string") ||
			strings.Contains(normalized, "utf8") ||
			strings.Contains(normalized, "utf-8") ||
			strings.Contains(normalized, "unicode") {
			return KindText, true
		}
	}
	return "", false
}

func (p Provider) findPngpaste() (string, error) {
	if p.PngpastePath != "" {
		return p.PngpastePath, nil
	}
	if path, err := exec.LookPath("pngpaste"); err == nil {
		return path, nil
	}
	for _, path := range []string{"/opt/homebrew/bin/pngpaste", "/usr/local/bin/pngpaste"} {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("pngpaste is required. Install it with: brew install pngpaste")
}
