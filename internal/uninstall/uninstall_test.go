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
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipctl"), filepath.Join(binDir, "clipportd"), sshConfig} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Run(Options{
		BinDir:      binDir,
		SSHConfig:   sshConfig,
		RemoveIterm: false,
		DryRun:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(result.Actions, "\n")
	for _, want := range []string{
		"dry run: no files changed",
		"would remove clipport SSH config blocks",
		"would remove " + filepath.Join(binDir, "clipctl"),
		"would remove " + filepath.Join(binDir, "clipportd"),
		"kept config, cache, and token files",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plan missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(binDir, "clipctl")); err != nil {
		t.Fatalf("dry run removed binary: %v", err)
	}
}

func TestUninstallRemovesBinariesAndSSHBlocks(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	sshConfig := filepath.Join(dir, "ssh_config")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipctl"), filepath.Join(binDir, "clipportd")} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	input := "Host keep\n    HostName keep.example.com\n# clipport begin dev\nHost dev\n# clipport end dev\n"
	if err := os.WriteFile(sshConfig, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(Options{
		BinDir:      binDir,
		SSHConfig:   sshConfig,
		RemoveIterm: false,
	}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipctl"), filepath.Join(binDir, "clipportd")} {
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

func TestUninstallUsesConfigFile(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "chosen-bin")
	sshConfig := filepath.Join(dir, "chosen_ssh_config")
	appPlist := filepath.Join(dir, "chosen-app.plist")
	appPath := filepath.Join(dir, "Clipport.app")
	configPath := filepath.Join(dir, "config.toml")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipctl"), filepath.Join(binDir, "clipportd"), filepath.Join(binDir, "clipport"), appPlist} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(sshConfig, []byte("Host keep\n# clipport begin dev\nHost dev\n# clipport end dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configText := `[local]
bin_dir = "` + filepath.ToSlash(binDir) + `"
ssh_config_path = "` + filepath.ToSlash(sshConfig) + `"
app_launchd_plist_path = "` + filepath.ToSlash(appPlist) + `"
app_path = "` + filepath.ToSlash(appPath) + `"

[local.iterm]
key = "0x69-0x180000"
configured = false
`
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Run(Options{
		ConfigPath: configPath,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(result.Actions, "\n")
	for _, want := range []string{
		"using config " + configPath,
		"would remove app launchd agent " + appPlist,
		"would remove clipport SSH config blocks from " + sshConfig,
		"would remove " + filepath.Join(binDir, "clipctl"),
		"would remove " + filepath.Join(binDir, "clipportd"),
		"would remove " + filepath.Join(binDir, "clipport"),
		"would remove app bundle " + appPath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config plan missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "iTerm hotkey") {
		t.Fatalf("config said iTerm was not configured, but plan touched it:\n%s", text)
	}
}

func TestUninstallUsesLocalSettingsWhenHostConfigIsInvalid(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "chosen-bin")
	sshConfig := filepath.Join(dir, "chosen_ssh_config")
	appPlist := filepath.Join(dir, "chosen-app.plist")
	configPath := filepath.Join(dir, "config.toml")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sshConfig, []byte("Host keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configText := `[local]
bin_dir = "` + filepath.ToSlash(binDir) + `"
ssh_config_path = "` + filepath.ToSlash(sshConfig) + `"
app_launchd_plist_path = "` + filepath.ToSlash(appPlist) + `"

[[hosts]]
name = "broken"
`
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Run(Options{
		ConfigPath: configPath,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(result.Actions, "\n")
	for _, want := range []string{
		"using config " + configPath,
		"would remove app launchd agent " + appPlist,
		"would remove clipport SSH config blocks from " + sshConfig,
		"would remove " + filepath.Join(binDir, "clipctl"),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("invalid-host config plan missing %q:\n%s", want, text)
		}
	}
}

func TestUninstallRemovesMenuArtifacts(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	sshConfig := filepath.Join(dir, "ssh_config")
	appPlist := filepath.Join(dir, "com.clipport.app.plist")
	appPath := filepath.Join(dir, "Clipport.app")
	if err := os.MkdirAll(filepath.Join(appPath, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(binDir, "clipctl"), filepath.Join(binDir, "clipportd"), filepath.Join(binDir, "clipport"), appPlist, filepath.Join(appPath, "Contents", "Info.plist")} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(sshConfig, []byte("Host keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(Options{
		BinDir:          binDir,
		SSHConfig:       sshConfig,
		AppLaunchdPlist: appPlist,
		AppPath:         appPath,
		RemoveIterm:     false,
	}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{filepath.Join(binDir, "clipctl"), filepath.Join(binDir, "clipportd"), filepath.Join(binDir, "clipport"), appPlist, appPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists or stat failed: %v", path, err)
		}
	}
}

func TestWithDefaultsUsesCurrentDefaultItermHotkey(t *testing.T) {
	opts := withDefaults(Options{CurrentExe: filepath.Join(t.TempDir(), "clipctl")})

	if opts.ItermKey != "0x76-0x120000" {
		t.Fatalf("ItermKey = %q, want Cmd-Shift-V key code", opts.ItermKey)
	}
}
