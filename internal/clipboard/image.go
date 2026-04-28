package clipboard

import (
	"errors"
	"os"
	"os/exec"
)

type ImageProvider struct {
	PngpastePath string
	Runner       func(name string, args ...string) ([]byte, error)
}

func (p ImageProvider) ReadPNG() ([]byte, error) {
	pngpaste, err := p.findPngpaste()
	if err != nil {
		return nil, err
	}
	runner := p.Runner
	if runner == nil {
		runner = func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).Output()
		}
	}
	data, err := runner(pngpaste, "-")
	if err != nil || len(data) == 0 {
		return nil, errors.New("no image found on the macOS clipboard")
	}
	return data, nil
}

func (p ImageProvider) findPngpaste() (string, error) {
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
