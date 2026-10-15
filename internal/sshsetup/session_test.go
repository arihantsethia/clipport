package sshsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSessionHookRepairsManagedCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	original := "# user comment\nHost dev\n    HostName dev.example\n    User alice\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSessionHook(path, "dev", "old-machine", "/old/bin/clipctl"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	backup, err := InstallSessionHook(path, "dev", "new-machine", "/new/bin/clipctl")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	saved, _ := os.ReadFile(backup)
	if string(saved) != string(before) {
		t.Fatal("backup does not preserve previous configuration")
	}
	if !strings.HasPrefix(string(after), original) || strings.Count(string(after), "# clipport session begin dev") != 1 {
		t.Fatalf("user config or block count changed: %s", after)
	}
	if strings.Contains(string(after), "/old/bin") || !strings.Contains(string(after), "'/new/bin/clipctl' session register --machine 'new-machine'") {
		t.Fatalf("hook not repaired: %s", after)
	}
}

func TestInstallSessionHookRejectsMalformedManagedBlocks(t *testing.T) {
	begin, end := "# clipport session begin dev\n", "# clipport session end dev\n"
	for _, content := range []string{begin + "Host dev\n", end, begin + begin + end, begin + end + begin + end, begin + "# clipport begin other\n" + end} {
		t.Run(strings.ReplaceAll(content, "\n", "|"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := InstallSessionHook(path, "dev", "dev", "/bin/clipctl"); err == nil {
				t.Fatal("expected malformed-block error")
			}
			after, _ := os.ReadFile(path)
			if string(after) != content {
				t.Fatal("malformed config changed")
			}
		})
	}
}

func TestInstallSessionHookPreservesConflictingUserSettings(t *testing.T) {
	for _, setting := range []string{"LocalCommand echo user-command", "PermitLocalCommand no"} {
		t.Run(setting, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			content := "Host *\n    " + setting + "\nHost dev\n    HostName dev.example\n"
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := InstallSessionHook(path, "dev", "dev", "/bin/clipctl")
			if err == nil || !strings.Contains(err.Error(), "override the Clipport session hook") {
				t.Fatalf("err=%v", err)
			}
			after, _ := os.ReadFile(path)
			if string(after) != content {
				t.Fatal("user settings changed")
			}
			matches, _ := filepath.Glob(path + ".clipport-*.bak")
			if len(matches) != 0 {
				t.Fatal("conflict created backup despite no write")
			}
		})
	}
}

func TestInstallSessionHookDetectsIncludedUserCommand(t *testing.T) {
	dir := t.TempDir()
	include := filepath.Join(dir, "included")
	if err := os.WriteFile(include, []byte("Host dev\n LocalCommand echo user-command\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	content := "Include \"" + include + "\"\nHost dev\n HostName dev.example\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := InstallSessionHook(path, "dev", "dev", "/bin/clipctl")
	if err == nil || !strings.Contains(err.Error(), "override the Clipport session hook") {
		t.Fatalf("err=%v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != content {
		t.Fatal("config changed")
	}
}

func TestInstallSessionHookWritesExpectedBlock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte("Host existing\n    HostName example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := InstallSessionHook(configPath, "devbox-public", "devbox", "/usr/local/bin/clipctl")
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected backup path")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# clipport session begin devbox-public\nHost devbox-public\n    PermitLocalCommand yes\n    LocalCommand '/usr/local/bin/clipctl' session register --machine 'devbox' --session-key \"${TERM_SESSION_ID:-}\" --ssh-alias '%n' --ssh-host '%h' --ssh-port '%p' --ssh-user '%r'\n# clipport session end devbox-public\n") {
		t.Fatalf("config missing session block:\n%s", text)
	}
}

func TestInstallSessionHookRejectsInvalidAlias(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := InstallSessionHook(configPath, "devbox\nProxyCommand evil", "devbox", "/usr/local/bin/clipctl")
	if err == nil {
		t.Fatal("expected invalid alias error")
	}
	if !strings.Contains(err.Error(), "invalid SSH host alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallSessionHookDoesNotTreatPrefixAliasAsInstalled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	existing := "# clipport session begin devbox\nHost devbox\n    PermitLocalCommand yes\n# clipport session end devbox\n"
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := InstallSessionHook(configPath, "dev", "dev", "/usr/local/bin/clipctl")
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected backup path")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# clipport session begin dev\nHost dev\n") {
		t.Fatalf("config missing dev session block:\n%s", text)
	}
}

func TestInstallSessionHookIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallSessionHook(configPath, "devbox-public", "devbox", "/usr/local/bin/clipctl"); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	backup, err := InstallSessionHook(configPath, "devbox-public", "devbox", "/usr/local/bin/clipctl")
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Fatalf("backup = %q, want empty on idempotent install", backup)
	}

	second, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Fatalf("config changed on second install:\nfirst:\n%s\nsecond:\n%s", string(first), string(second))
	}
}
