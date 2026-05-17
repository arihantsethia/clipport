package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileSinkAppendsJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	sink := FileSink{
		Path: path,
		Now:  func() time.Time { return time.Date(2026, 5, 17, 9, 10, 11, 0, time.UTC) },
	}

	if err := sink.Record(Event{Op: "paste", Host: "zyra", Route: "public", OK: true, Bytes: 12}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Record(Event{Op: "paste", Host: "zyra", Route: "public", OK: false, Error: "upload_failed"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2:\n%s", len(lines), data)
	}
	var event Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatal(err)
	}
	if event.At != "2026-05-17T09:10:11Z" || event.Op != "paste" || !event.OK || event.Bytes != 12 {
		t.Fatalf("event = %+v", event)
	}
}

func TestFileSinkKeepsRecentEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte(
		`{"host":"old"}`+"\n"+
			`{"host":"middle"}`+"\n"+
			`{"host":"new"}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := trim(path, 2); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"host":"old"`) || !strings.Contains(text, `"host":"middle"`) || !strings.Contains(text, `"host":"new"`) {
		t.Fatalf("unexpected retained events:\n%s", text)
	}
}
