package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDryRunPlansDefaultUninstallWithoutRemovingData(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	sshConfig := filepath.Join(dir, "ssh_config")
	plist := filepath.Join(dir, "com.clipport.clipportd.plist")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipport"), filepath.Join(binDir, "clipportd"), sshConfig, plist} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Run(Options{
		BinDir:       binDir,
		SSHConfig:    sshConfig,
		LaunchdPlist: plist,
		RemoveIterm:  false,
		DryRun:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(result.Actions, "\n")
	for _, want := range []string{
		"dry run: no files changed",
		"would remove clipport SSH config blocks",
		"would remove " + filepath.Join(binDir, "clipport"),
		"would remove " + filepath.Join(binDir, "clipportd"),
		"kept config, cache, and token files",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plan missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(binDir, "clipport")); err != nil {
		t.Fatalf("dry run removed binary: %v", err)
	}
}

func TestUninstallRemovesBinariesAndSSHBlocks(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	sshConfig := filepath.Join(dir, "ssh_config")
	plist := filepath.Join(dir, "com.clipport.clipportd.plist")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipport"), filepath.Join(binDir, "clipportd"), plist} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	input := "Host keep\n    HostName keep.example.com\n# clipport begin dev\nHost dev\n# clipport end dev\n"
	if err := os.WriteFile(sshConfig, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(Options{
		BinDir:       binDir,
		SSHConfig:    sshConfig,
		LaunchdPlist: plist,
		RemoveIterm:  false,
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipport"), filepath.Join(binDir, "clipportd"), plist} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists or stat failed: %v", path, err)
		}
	}
	data, err := os.ReadFile(sshConfig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "clipport") || !strings.Contains(string(data), "Host keep") {
		t.Fatalf("unexpected ssh config after uninstall:\n%s", string(data))
	}
}

func TestUninstallUsesInstallManifest(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "chosen-bin")
	sshConfig := filepath.Join(dir, "chosen_ssh_config")
	plist := filepath.Join(dir, "chosen.plist")
	manifest := filepath.Join(dir, "install.toml")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipport"), filepath.Join(binDir, "clipportd"), plist} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(sshConfig, []byte("Host keep\n# clipport begin dev\nHost dev\n# clipport end dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestText := `bin_dir = "` + filepath.ToSlash(binDir) + `"
ssh_config_path = "` + filepath.ToSlash(sshConfig) + `"
launchd_plist_path = "` + filepath.ToSlash(plist) + `"
iterm_key = "0x69-0x180000"
iterm_configured = false
session_hooks_configured = true
`
	if err := os.WriteFile(manifest, []byte(manifestText), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Run(Options{
		ManifestPath: manifest,
		DryRun:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(result.Actions, "\n")
	for _, want := range []string{
		"using install manifest " + manifest,
		"would remove launchd agent " + plist,
		"would remove clipport SSH config blocks from " + sshConfig,
		"would remove " + filepath.Join(binDir, "clipport"),
		"would remove " + filepath.Join(binDir, "clipportd"),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manifest plan missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "iTerm hotkey") {
		t.Fatalf("manifest said iTerm was not configured, but plan touched it:\n%s", text)
	}
}

func TestWithDefaultsUsesCurrentDefaultItermHotkey(t *testing.T) {
	opts := withDefaults(Options{CurrentExe: filepath.Join(t.TempDir(), "clipport")})

	if opts.ItermKey != "0x76-0x120000" {
		t.Fatalf("ItermKey = %q, want Cmd-Shift-V key code", opts.ItermKey)
	}
}
