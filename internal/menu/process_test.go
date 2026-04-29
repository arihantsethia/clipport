package menu

import (
	"os/exec"
	"testing"
)

func TestDaemonProcessStartBuildsDaemonCommand(t *testing.T) {
	var gotName string
	var gotArgs []string
	process := DaemonProcess{
		BinPath:    "/Users/me/.local/bin/clipportd",
		ConfigPath: "/Users/me/.config/clipport/config.toml",
		HTTPAddr:   "127.0.0.1:18765",
		Command: func(name string, args ...string) *exec.Cmd {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return exec.Command("true")
		},
	}

	if err := process.Start(); err != nil {
		t.Fatal(err)
	}

	if gotName != "/Users/me/.local/bin/clipportd" {
		t.Fatalf("name = %q", gotName)
	}
	wantPrefix := []string{"--config", "/Users/me/.config/clipport/config.toml", "--http", "127.0.0.1:18765", "--parent-pid"}
	if len(gotArgs) != len(wantPrefix)+1 {
		t.Fatalf("args = %#v, want prefix %#v plus pid", gotArgs, wantPrefix)
	}
	for i := range wantPrefix {
		if gotArgs[i] != wantPrefix[i] {
			t.Fatalf("args = %#v, want prefix %#v", gotArgs, wantPrefix)
		}
	}
	if gotArgs[len(gotArgs)-1] == "" {
		t.Fatalf("parent pid missing in args %#v", gotArgs)
	}
}
