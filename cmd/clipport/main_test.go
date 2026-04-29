package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/daemon"
)

func TestPasteErrorMessageHidesSocketDetailsInNormalMode(t *testing.T) {
	msg := pasteErrorMessage(daemon.Response{}, errors.New("dial unix /tmp/clipport/501/clipportd.sock: connect: no such file or directory"), false)
	if msg != daemon.PasteUnavailable {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, "/tmp/clipport") || strings.Contains(msg, "dial unix") {
		t.Fatalf("message leaked diagnostic detail: %q", msg)
	}
}

func TestPasteErrorMessageShowsSocketDetailsInDebugMode(t *testing.T) {
	msg := pasteErrorMessage(daemon.Response{}, errors.New("dial unix /tmp/clipport/501/clipportd.sock: connect: no such file or directory"), true)
	if !strings.Contains(msg, daemon.PasteUnavailable) || !strings.Contains(msg, "dial unix") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPasteErrorMessageUsesDaemonDebugInDebugMode(t *testing.T) {
	resp := daemon.Response{Error: daemon.PasteUnavailable, Debug: "clipboard has no image or text"}
	msg := pasteErrorMessage(resp, errors.New(resp.Error), true)
	if !strings.Contains(msg, "clipboard has no image or text") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPasteOutputReturnsTextBeforePath(t *testing.T) {
	resp := daemon.Response{Path: "/tmp/clipport/file.txt", Text: "hello"}
	if got := pasteOutput(resp); got != "hello" {
		t.Fatalf("output = %q", got)
	}
}
