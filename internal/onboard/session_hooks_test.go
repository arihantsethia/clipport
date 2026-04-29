package onboard

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestMaybeInstallSessionHooksAcceptsDefaultYes(t *testing.T) {
	var called bool
	var out bytes.Buffer
	err := MaybeInstallSessionHooks("cfg", "ssh", "/bin/clipctl", strings.NewReader("\n"), &out, func(configPath, sshConfigPath, clipctlBin string) ([]string, error) {
		called = true
		if configPath != "cfg" || sshConfigPath != "ssh" || clipctlBin != "/bin/clipctl" {
			t.Fatalf("unexpected args: %q %q %q", configPath, sshConfigPath, clipctlBin)
		}
		return []string{"installed session hook for devbox -> devbox"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected installer to be called")
	}
	if got := out.String(); !strings.Contains(got, "Enable automatic SSH session matching? [Y/n]") || !strings.Contains(got, "installed session hook for devbox -> devbox") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestMaybeInstallSessionHooksSkipsOnNo(t *testing.T) {
	var called bool
	var out bytes.Buffer
	err := MaybeInstallSessionHooks("cfg", "ssh", "/bin/clipctl", strings.NewReader("n\n"), &out, func(configPath, sshConfigPath, clipctlBin string) ([]string, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("did not expect installer to be called")
	}
}

func TestMaybeInstallSessionHooksSkipsOnEmptyEOF(t *testing.T) {
	var called bool
	var out bytes.Buffer
	err := MaybeInstallSessionHooks("cfg", "ssh", "/bin/clipctl", strings.NewReader(""), &out, func(configPath, sshConfigPath, clipctlBin string) ([]string, error) {
		called = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("did not expect installer to run on EOF")
	}
}

func TestMaybeInstallSessionHooksPropagatesInstallError(t *testing.T) {
	want := errors.New("boom")
	err := MaybeInstallSessionHooks("cfg", "ssh", "/bin/clipctl", strings.NewReader("y\n"), &bytes.Buffer{}, func(configPath, sshConfigPath, clipctlBin string) ([]string, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
