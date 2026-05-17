package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Event struct {
	At         string `json:"at,omitempty"`
	Op         string `json:"op"`
	Host       string `json:"host,omitempty"`
	Route      string `json:"route,omitempty"`
	Target     string `json:"target,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Bytes      int    `json:"bytes,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	OK         bool   `json:"ok"`
	Path       string `json:"path,omitempty"`
	Error      string `json:"error,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type Sink interface {
	Record(Event) error
}

type FileSink struct {
	Path string
	Now  func() time.Time
}

const defaultMaxEvents = 500

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "clipport", "events.jsonl")
}

func (s FileSink) Record(e Event) error {
	path := s.Path
	if path == "" {
		path = DefaultPath()
	}
	if e.At == "" {
		now := time.Now
		if s.Now != nil {
			now = s.Now
		}
		e.At = now().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(e); err != nil {
		return err
	}
	return trim(path, defaultMaxEvents)
}

func trim(path string, max int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= max {
		return nil
	}
	kept := strings.Join(lines[len(lines)-max:], "\n") + "\n"
	return os.WriteFile(path, []byte(kept), 0o600)
}
