package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunStartsMenuAppWithNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	started := false

	code := run([]string{"clipport"}, &stdout, &stderr, func() {
		started = true
	})

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !started {
		t.Fatal("menu app did not start")
	}
	if stdout.String() != "" || stderr.String() != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsPasteCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	started := false

	code := run([]string{"clipport", "paste"}, &stdout, &stderr, func() {
		started = true
	})

	if code == 0 {
		t.Fatal("expected nonzero exit")
	}
	if started {
		t.Fatal("menu app started for paste command")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "clipport: use clipctl paste") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestRunRejectsUnknownArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	started := false

	code := run([]string{"clipport", "status"}, &stdout, &stderr, func() {
		started = true
	})

	if code == 0 {
		t.Fatal("expected nonzero exit")
	}
	if started {
		t.Fatal("menu app started for unknown argument")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if got := stderr.String(); !strings.Contains(got, "clipport: use clipctl paste") {
		t.Fatalf("stderr = %q", got)
	}
}
