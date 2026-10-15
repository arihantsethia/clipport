package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/uninstall"
)

func TestInstallerHotSwapsOnlyWhenLaunchAgentWasRunning(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	probe := `launchctl print "gui/$(id -u)/$app_label"`
	if !strings.Contains(text, probe) {
		t.Fatalf("install.sh missing running launch agent probe %q", probe)
	}
	probeAt := strings.Index(text, "if app_launch_agent_running || app_process_running; then")
	restartAt := strings.Index(text, `"$bin_dir/clipctl" restart --config "$config_path"`)
	if probeAt < 0 || restartAt < 0 {
		t.Fatalf("install.sh missing running-state restart flow")
	}
	if !strings.Contains(text, `pgrep -f "$app_path/Contents/MacOS/clipport"`) {
		t.Fatalf("install.sh missing running app process probe")
	}
	if probeAt > restartAt {
		t.Fatalf("install.sh must capture running state before restarting")
	}
	if strings.Contains(text, `"$bin_dir/clipctl" start --config "$config_path"`) {
		t.Fatalf("install.sh should not start a stopped install during binary swap")
	}
}

func TestInstallerReportsInstallOrUpgrade(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	detectAt := strings.Index(text, "detect_install_action()")
	buildAt := strings.Index(text, "go build -o \"$build_dir/clipctl\" ./cmd/clipctl")
	if detectAt < 0 || buildAt < 0 || detectAt > buildAt {
		t.Fatalf("install.sh should detect install action before replacing binaries")
	}
	for _, want := range []string{
		`install_result="installed"`,
		`install_result="upgraded"`,
		`say "Clipport is ${install_result}."`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
}

func TestRecordItermConfiguredUpdatesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".config", "clipport", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[local]\nbin_dir = \"/tmp/bin\"\n\n[local.iterm]\nconfigured = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recordItermConfigured(configPath, "0x76-0x120000"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Local.Iterm.Configured || cfg.Local.Iterm.Key != "0x76-0x120000" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestRecordInstallSettingsUpdatesSSHConfigPath(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
default_host = "devbox"

[[hosts]]
name = "devbox"

[[hosts.routes]]
name = "lan"
ssh_target = "devbox-lan"
priority = 10
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recordInstallSettings(configPath, "", "/tmp/custom-ssh-config", "", "", "", ""); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Local.SSHConfigPath != "/tmp/custom-ssh-config" {
		t.Fatalf("SSHConfigPath = %q", cfg.Local.SSHConfigPath)
	}
}

func TestRecordInstallSettingsRejectsNonLoopbackHTTP(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	err := recordInstallSettings(configPath, "", "", "", "", "0.0.0.0:18765", "")
	if err == nil {
		t.Fatal("expected non-loopback HTTP address to fail")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v", err)
	}
}

func TestRecordInstallSettingsAcceptsIPv6LoopbackHTTP(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := recordInstallSettings(configPath, "", "", "", "", "[::1]:18765", ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Local.HTTPAddr != "[::1]:18765" {
		t.Fatalf("HTTPAddr = %q", cfg.Local.HTTPAddr)
	}
}

func TestRecordHomebrewInstallCreatesUserAppAndPreservesHTTP(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	prefix := filepath.Join(root, "opt", "clipport")
	configPath := filepath.Join(home, ".config", "clipport", "config.toml")
	createHomebrewInstallForTest(t, prefix)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[local]\nhttp_addr = \"127.0.0.1:19999\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := recordHomebrewInstall(configPath, "/tmp/ssh_config", prefix, home); err != nil {
		t.Fatal(err)
	}

	appLink := filepath.Join(home, "Applications", "Clipport.app")
	target, err := os.Readlink(appLink)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(prefix, "libexec", "Clipport.app") {
		t.Fatalf("app link = %q", target)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Local.BinDir != filepath.Join(prefix, "bin") {
		t.Fatalf("BinDir = %q", cfg.Local.BinDir)
	}
	if cfg.Local.AppLaunchdPlistPath != filepath.Join(prefix, "libexec", uninstall.AppLaunchdLabel+".plist") {
		t.Fatalf("AppLaunchdPlistPath = %q", cfg.Local.AppLaunchdPlistPath)
	}
	if cfg.Local.AppPath != appLink {
		t.Fatalf("AppPath = %q", cfg.Local.AppPath)
	}
	if cfg.Local.HTTPAddr != "127.0.0.1:19999" {
		t.Fatalf("HTTPAddr = %q", cfg.Local.HTTPAddr)
	}
	if cfg.Local.SSHConfigPath != "/tmp/ssh_config" {
		t.Fatalf("SSHConfigPath = %q", cfg.Local.SSHConfigPath)
	}
	if cfg.Local.Iterm.Key != "0x76-0x120000" {
		t.Fatalf("Iterm.Key = %q", cfg.Local.Iterm.Key)
	}
}

func TestEnsureUserAppLinkRejectsNonClipportApp(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	prefix := filepath.Join(root, "opt", "clipport")
	createHomebrewInstallForTest(t, prefix)
	appLink := filepath.Join(home, "Applications", "Clipport.app")
	if err := os.MkdirAll(filepath.Join(appLink, "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appLink, "Contents", "Info.plist"), []byte("<string>com.example.other</string>"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := recordHomebrewInstall(filepath.Join(home, ".config", "clipport", "config.toml"), "", prefix, home)
	if err == nil || !strings.Contains(err.Error(), "already exists and is not Clipport") {
		t.Fatalf("err = %v", err)
	}
}

func TestHomebrewPrefixForExecutableDetectsOptInstall(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "opt", "clipport")
	createHomebrewInstallForTest(t, prefix)
	exe := filepath.Join(root, "Cellar", "clipport", "0.1.0", "bin", "clipctl")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("clipctl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(prefix, "bin", "clipctl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(prefix, "bin", "clipctl")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOMEBREW_PREFIX", root)

	got, ok := homebrewPrefixForExecutable(exe)
	if !ok || got != prefix {
		t.Fatalf("prefix=%q ok=%v", got, ok)
	}
}

func TestBuildUpdateInvocationCurlsCanonicalInstaller(t *testing.T) {
	name, args, env := buildUpdateInvocation("/tmp/config.toml", config.LocalConfig{
		BinDir:   "/tmp/bin",
		HTTPAddr: "127.0.0.1:18765",
		Iterm:    config.ItermConfig{Key: "0x76-0x120000"},
	})
	if name != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("name=%q args=%q", name, args)
	}
	script := args[1]
	for _, want := range []string{
		`repo_ref="${CLIPPORT_REF:-main}"`,
		`curl -fsSL "https://raw.githubusercontent.com/arihantsethia/clipport/${repo_ref}/install.sh" | sh`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("update script missing %q:\n%s", want, script)
		}
	}
	for _, want := range []string{
		"CLIPPORT_BIN=/tmp/bin",
		"CLIPPORT_CONFIG=/tmp/config.toml",
		"CLIPPORT_HTTP=127.0.0.1:18765",
		"CLIPPORT_ITERM_KEY=0x76-0x120000",
	} {
		if !envContains(env, want) {
			t.Fatalf("env missing %q", want)
		}
	}
}

func TestBuildUpdateInvocationIgnoresLocalCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "clipctl"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "install.sh"),
		filepath.Join(root, "go.mod"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	subdir := filepath.Join(root, "internal", "daemon")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}

	name, args, _ := buildUpdateInvocation("/tmp/config.toml", config.LocalConfig{})
	if name != "sh" || len(args) != 2 || args[0] != "-c" {
		t.Fatalf("name=%q args=%q", name, args)
	}
	if strings.Contains(args[1], root) || strings.Contains(args[1], "git clone") {
		t.Fatalf("update should not use local checkout or git clone:\n%s", args[1])
	}
}

func envContains(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func createHomebrewInstallForTest(t *testing.T, prefix string) {
	t.Helper()
	for _, dir := range []string{
		filepath.Join(prefix, "bin"),
		filepath.Join(prefix, "libexec", "Clipport.app", "Contents", "MacOS"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(prefix, "bin", "clipctl"),
		filepath.Join(prefix, "libexec", "Clipport.app", "Contents", "MacOS", "clipport"),
		filepath.Join(prefix, "libexec", uninstall.AppLaunchdLabel+".plist"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}
