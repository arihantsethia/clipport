package token

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clipport", "token")
}

func LoadOrCreate(path string) (string, error) {
	if path == "" {
		path = DefaultPath()
	}
	value, err := Load(path)
	if err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", err
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	value = base64.RawURLEncoding.EncodeToString(b[:])
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	return value, nil
}

func Load(path string) (string, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
