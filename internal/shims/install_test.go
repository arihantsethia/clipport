package shims

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// These tests run the actual remote POSIX-shell payload against an isolated Linux
// home. The client tokens represent independent clients, not shared credentials.
// They do not claim to exercise macOS clipboard/launchd or an SSH transport.
func runInstall(home, token string, port int) (string, error) {
	cmd := exec.Command("sh")
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(renderInstallPayload(token, port))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func linuxHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux remote-shell integration test")
	}
	return t.TempDir()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLinuxIndependentClientsDoNotOverwriteRemoteIdentity(t *testing.T) {
	home := linuxHome(t)
	first, second := "client_A_independent_identity", "client_B_independent_identity"
	if out, err := runInstall(home, first, 18765); err != nil {
		t.Fatalf("first install: %v: %s", err, out)
	}
	tokenPath := filepath.Join(home, ".config/clipport/token")
	shimPath := filepath.Join(home, ".local/bin/clipport-wl-paste")
	originalToken, originalShim := readFile(t, tokenPath), readFile(t, shimPath)
	out, err := runInstall(home, second, 28765)
	if err == nil || !strings.Contains(out, "remote token differs") {
		t.Fatalf("expected conflict, got %v: %s", err, out)
	}
	if strings.Contains(out, first) || strings.Contains(out, second) {
		t.Fatal("diagnostic leaked token")
	}
	if readFile(t, tokenPath) != originalToken || readFile(t, shimPath) != originalShim {
		t.Fatal("conflicting client changed remote identity or shim")
	}
	if out, err := runInstall(home, first, 18765); err != nil {
		t.Fatalf("idempotent install: %v: %s", err, out)
	}
	if readFile(t, tokenPath) != originalToken || readFile(t, shimPath) != originalShim {
		t.Fatal("same-client setup was not idempotent")
	}
	for path, mode := range map[string]os.FileMode{tokenPath: 0600, shimPath: 0755} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != mode {
			t.Fatalf("%s mode %o", path, st.Mode().Perm())
		}
	}
	if strings.Contains(originalShim, first) {
		t.Fatal("executable shim contains token")
	}
}

func TestLinuxConcurrentClientsHaveExactlyOneOwner(t *testing.T) {
	home := linuxHome(t)
	tokens := []string{"independent_client_A", "independent_client_B"}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	outs := make([]string, 2)
	start := make(chan struct{})
	for i := range tokens {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-start; outs[i], errs[i] = runInstall(home, tokens[i], 18765+i) }(i)
	}
	close(start)
	wg.Wait()
	successes := 0
	for i, err := range errs {
		if err == nil {
			successes++
			if got := readFile(t, filepath.Join(home, ".config/clipport/token")); got != tokens[i]+"\n" {
				t.Fatal("winner does not own token")
			}
			if !strings.Contains(readFile(t, filepath.Join(home, ".local/bin/clipport-wl-paste")), "CLIPPORT_PORT=${CLIPPORT_PORT:-"+[]string{"18765", "18766"}[i]+"}") {
				t.Fatal("loser replaced winner shim")
			}
		} else if !strings.Contains(outs[i], "remote token differs") {
			t.Fatalf("unexpected failure: %s", outs[i])
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful installs; %v", successes, errs)
	}
}

func TestLinuxUnsafeExistingTokensFailClosed(t *testing.T) {
	for _, kind := range []string{"empty", "multiline", "missing-newline", "unreadable", "directory", "symlink", "dangling-symlink"} {
		t.Run(kind, func(t *testing.T) {
			home := linuxHome(t)
			dir := filepath.Join(home, ".config/clipport")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "token")
			outside := filepath.Join(home, "outside")
			if err := os.WriteFile(outside, []byte("untouched\n"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "empty":
				err = os.WriteFile(path, nil, 0600)
			case "multiline":
				err = os.WriteFile(path, []byte("client_A\nextra\n"), 0600)
			case "missing-newline":
				err = os.WriteFile(path, []byte("client_A"), 0600)
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses read permission checks")
				}
				err = os.WriteFile(path, []byte("client_A\n"), 0000)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink":
				err = os.Symlink(outside, path)
			case "dangling-symlink":
				err = os.Symlink(outside+"-missing", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			out, err := runInstall(home, "client_A", 18765)
			if err == nil {
				t.Fatalf("unsafe %s token accepted: %s", kind, out)
			}
			after, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("unsafe token mutated")
			}
			if readFile(t, outside) != "untouched\n" {
				t.Fatal("symlink target changed")
			}
			if _, err := os.Lstat(filepath.Join(home, ".local/bin/clipport-wl-paste")); !os.IsNotExist(err) {
				t.Fatal("shim created despite refusal")
			}
		})
	}
}

func TestLinuxRefusesSymlinkShimAndUnrelatedCommands(t *testing.T) {
	for _, name := range []string{"clipport-wl-paste", "wl-paste", "xclip"} {
		t.Run(name, func(t *testing.T) {
			home := linuxHome(t)
			bin := filepath.Join(home, ".local/bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(home, "original")
			if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(bin, name)); err != nil {
				t.Fatal(err)
			}
			if out, err := runInstall(home, "client_A", 18765); err == nil {
				t.Fatalf("accepted unrelated file: %s", out)
			}
			if readFile(t, outside) != "untouched" {
				t.Fatal("original changed")
			}
			if _, err := os.Stat(filepath.Join(home, ".config/clipport/token")); !os.IsNotExist(err) {
				t.Fatal("published token before preflight finished")
			}
		})
	}
}

func TestLinuxSameClientRepairsMissingShim(t *testing.T) {
	home := linuxHome(t)
	if out, err := runInstall(home, "client_A", 18765); err != nil {
		t.Fatalf("install: %v: %s", err, out)
	}
	shim := filepath.Join(home, ".local/bin/clipport-wl-paste")
	if err := os.Remove(shim); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".local/bin/wl-paste")); err != nil {
		t.Fatal(err)
	}
	if out, err := runInstall(home, "client_A", 18765); err != nil {
		t.Fatalf("repair: %v: %s", err, out)
	}
	if !strings.Contains(readFile(t, shim), "/v1/clipboard") {
		t.Fatal("shim not repaired")
	}
	if _, err := os.Stat(filepath.Join(home, ".local/bin/wl-paste")); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxExistingLockIsNotBroken(t *testing.T) {
	home := linuxHome(t)
	lock := filepath.Join(home, ".config/clipport/.shim-install.lock")
	if err := os.MkdirAll(lock, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(lock, "owner")
	if err := os.WriteFile(marker, []byte("other install"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runInstall(home, "client_A", 18765)
	if err == nil || !strings.Contains(out, "setup is busy") {
		t.Fatalf("expected bounded lock refusal: %v: %s", err, out)
	}
	if readFile(t, marker) != "other install" {
		t.Fatal("another install's lock changed")
	}
	if _, err := os.Stat(filepath.Join(home, ".config/clipport/token")); !os.IsNotExist(err) {
		t.Fatal("token written while locked")
	}
}

func TestLinuxTokenMatchingOldHeredocDelimiter(t *testing.T) {
	home := linuxHome(t)
	if out, err := runInstall(home, "CLIPPORT_TOKEN", 18765); err != nil {
		t.Fatalf("valid URL-safe token refused: %v: %s", err, out)
	}
	if readFile(t, filepath.Join(home, ".config/clipport/token")) != "CLIPPORT_TOKEN\n" {
		t.Fatal("token changed")
	}
}
