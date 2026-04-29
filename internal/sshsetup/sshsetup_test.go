package sshsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHostAliasRejectsInjection(t *testing.T) {
	bad := []string{
		"devbox\nProxyCommand evil",
		"devbox public",
		"devbox\tpublic",
	}
	for _, host := range bad {
		if hostAliasRE.MatchString(host) {
			t.Fatalf("host alias %q was accepted", host)
		}
	}
}

func TestValidateHostAliasAllowsCommonAliases(t *testing.T) {
	good := []string{"devbox", "devbox-public", "host.example.com", "host_1", "host+test"}
	for _, host := range good {
		if !hostAliasRE.MatchString(host) {
			t.Fatalf("host alias %q was rejected", host)
		}
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

func TestInstallForwardDoesNotTreatPrefixAliasAsInstalled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	existing := "# clipport begin devbox\nHost devbox\n    RemoteForward 127.0.0.1:18765 127.0.0.1:18765\n# clipport end devbox\n"
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := InstallForward(configPath, "dev", 18765)
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
	if !strings.Contains(text, "# clipport begin dev\nHost dev\n") {
		t.Fatalf("config missing dev block:\n%s", text)
	}
}

func TestInstallForwardIsIdempotentForExactMarker(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := InstallForward(configPath, "dev", 18765); err != nil {
		t.Fatal(err)
	}
	_, err := InstallForward(configPath, "dev", 18765)
	if err == nil {
		t.Fatal("expected already installed error")
	}
	if !strings.Contains(err.Error(), ErrForwardAlreadyInstalled.Error()) {
		t.Fatalf("unexpected error: %v", err)
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

func TestRemoveAllClipportBlocksPreservesUserConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	input := strings.Join([]string{
		"Host personal",
		"    HostName personal.example.com",
		"",
		"# clipport begin devbox",
		"Host devbox",
		"    RemoteForward 127.0.0.1:18765 127.0.0.1:18765",
		"# clipport end devbox",
		"",
		"# clipport session begin devbox",
		"Host devbox",
		"    PermitLocalCommand yes",
		"# clipport session end devbox",
		"",
		"Host work",
		"    HostName work.example.com",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := RemoveAllClipportBlocks(configPath)
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
	if strings.Contains(text, "clipport") || strings.Contains(text, "RemoteForward") || strings.Contains(text, "PermitLocalCommand") {
		t.Fatalf("config still contains clipport block:\n%s", text)
	}
	if !strings.Contains(text, "Host personal\n    HostName personal.example.com") || !strings.Contains(text, "Host work\n    HostName work.example.com") {
		t.Fatalf("user config was not preserved:\n%s", text)
	}
}

func TestRemoveForwardOnlyRemovesMatchingAlias(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	input := "# clipport begin devbox\nHost devbox\n# clipport end devbox\n# clipport begin staging\nHost staging\n# clipport end staging\n"
	if err := os.WriteFile(configPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := RemoveForward(configPath, "devbox"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "devbox") {
		t.Fatalf("devbox block still present:\n%s", text)
	}
	if !strings.Contains(text, "# clipport begin staging") {
		t.Fatalf("staging block was removed:\n%s", text)
	}
}
