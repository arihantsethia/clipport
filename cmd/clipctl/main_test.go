package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/uninstall"
)

func TestHelpFlagPrintsCommandUsage(t *testing.T) {
	output, err := runClipctlForTest("--help")
	if err != nil {
		t.Fatalf("clipctl --help failed: %v\n%s", err, output)
	}
	assertCommandUsage(t, output)
}

func TestHelpCommandPrintsCommandUsage(t *testing.T) {
	output, err := runClipctlForTest("help")
	if err != nil {
		t.Fatalf("clipctl help failed: %v\n%s", err, output)
	}
	assertCommandUsage(t, output)
}

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

func TestMaybeRestartInstalledAppRestartsRunningLaunchAgent(t *testing.T) {
	var calls []string
	err := maybeStartOrRestartInstalledApp(
		config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"},
		func(config.LocalConfig, int) (bool, error) { return true, nil },
		func(name string, args ...string) error {
			if name != "launchctl" {
				t.Fatalf("name = %q", name)
			}
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		func([]string) error { return nil },
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootout gui/501 /tmp/com.clipport.app.plist",
		"launchctl bootstrap gui/501 /tmp/com.clipport.app.plist",
		"launchctl kickstart -k gui/501/" + uninstall.AppLaunchdLabel,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestMaybeRestartInstalledAppStartsStoppedLaunchAgent(t *testing.T) {
	var calls []string
	err := maybeStartOrRestartInstalledApp(
		config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"},
		func(config.LocalConfig, int) (bool, error) { return false, nil },
		func(name string, args ...string) error {
			if name != "launchctl" {
				t.Fatalf("name = %q", name)
			}
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		func([]string) error {
			t.Fatal("did not expect process cleanup for stopped app")
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootstrap gui/501 /tmp/com.clipport.app.plist",
		"launchctl kickstart -k gui/501/" + uninstall.AppLaunchdLabel,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestStartInstalledAppBootstrapsAndKickstarts(t *testing.T) {
	var calls []string
	err := startInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"}, nil
		},
		"",
		func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootstrap gui/501 /tmp/com.clipport.app.plist",
		"launchctl kickstart -k gui/501/" + uninstall.AppLaunchdLabel,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestStartInstalledAppIgnoresAlreadyLoadedBootstrapError(t *testing.T) {
	var calls []string
	err := startInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"}, nil
		},
		"",
		func(name string, args ...string) error {
			call := strings.Join(append([]string{name}, args...), " ")
			calls = append(calls, call)
			if strings.Contains(call, "bootstrap") {
				return errors.New("service already loaded")
			}
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.Contains(calls[1], "kickstart") {
		t.Fatalf("calls = %q", calls)
	}
}

func TestStartInstalledAppReturnsRealBootstrapError(t *testing.T) {
	err := startInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"}, nil
		},
		"",
		func(name string, args ...string) error {
			if len(args) > 0 && args[0] == "bootstrap" {
				return errors.New("invalid plist")
			}
			return nil
		},
		501,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid plist") {
		t.Fatalf("err = %v", err)
	}
}

func TestStopInstalledAppBootsOut(t *testing.T) {
	var calls []string
	err := stopInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{
				BinDir:              "/tmp/bin",
				AppLaunchdPlistPath: "/tmp/com.clipport.app.plist",
				AppPath:             "/tmp/Clipport.app",
			}, nil
		},
		"",
		func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		func([]string) error { return nil },
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootout gui/501 /tmp/com.clipport.app.plist",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestStopInstalledAppCleansInstalledClipportProcesses(t *testing.T) {
	var cleaned []string
	err := stopInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{
				BinDir:              "/tmp/bin",
				AppLaunchdPlistPath: "/tmp/com.clipport.app.plist",
				AppPath:             "/tmp/Clipport.app",
			}, nil
		},
		"",
		func(name string, args ...string) error { return nil },
		func(paths []string) error {
			cleaned = append(cleaned, paths...)
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/Clipport.app/Contents/MacOS/clipport", "/tmp/bin/clipport"}
	if strings.Join(cleaned, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestStopInstalledAppIgnoresNotLoadedError(t *testing.T) {
	err := stopInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"}, nil
		},
		"",
		func(name string, args ...string) error {
			return errors.New("service not loaded")
		},
		func([]string) error { return nil },
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStopInstalledAppReturnsRealBootoutError(t *testing.T) {
	err := stopInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{AppLaunchdPlistPath: "/tmp/com.clipport.app.plist"}, nil
		},
		"",
		func(name string, args ...string) error {
			return errors.New("permission denied")
		},
		func([]string) error { return nil },
		501,
	)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestStopInstalledAppCleansWhenLaunchAgentPlistIsMissing(t *testing.T) {
	var cleaned []string
	missingPlist := filepath.Join(t.TempDir(), "missing.plist")
	err := stopInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{
				BinDir:              "/tmp/bin",
				AppLaunchdPlistPath: missingPlist,
				AppPath:             "/tmp/Clipport.app",
			}, nil
		},
		"",
		func(name string, args ...string) error {
			return errors.New("exit status 5: Boot-out failed: 5: Input/output error")
		},
		func(paths []string) error {
			cleaned = append(cleaned, paths...)
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/Clipport.app/Contents/MacOS/clipport", "/tmp/bin/clipport"}
	if strings.Join(cleaned, "\n") != strings.Join(want, "\n") {
		t.Fatalf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestRestartInstalledAppCleansStaleProcessesBeforeBootstrap(t *testing.T) {
	var calls []string
	err := restartInstalledAppWithLocal(
		config.LocalConfig{
			BinDir:              "/tmp/bin",
			AppLaunchdPlistPath: "/tmp/com.clipport.app.plist",
			AppPath:             "/tmp/Clipport.app",
		},
		func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			return nil
		},
		func(paths []string) error {
			calls = append(calls, "cleanup "+strings.Join(paths, ","))
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootout gui/501 /tmp/com.clipport.app.plist",
		"cleanup /tmp/Clipport.app/Contents/MacOS/clipport,/tmp/bin/clipport",
		"launchctl bootstrap gui/501 /tmp/com.clipport.app.plist",
		"launchctl kickstart -k gui/501/" + uninstall.AppLaunchdLabel,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestRestartInstalledAppIgnoresStaleBootoutError(t *testing.T) {
	var calls []string
	err := restartInstalledAppWithLocal(
		config.LocalConfig{
			BinDir:              "/tmp/bin",
			AppLaunchdPlistPath: "/tmp/com.clipport.app.plist",
			AppPath:             "/tmp/Clipport.app",
		},
		func(name string, args ...string) error {
			calls = append(calls, strings.Join(append([]string{name}, args...), " "))
			if len(calls) == 1 {
				return errors.New("exit status 5: Boot-out failed: 5: Input/output error")
			}
			return nil
		},
		func(paths []string) error {
			calls = append(calls, "cleanup "+strings.Join(paths, ","))
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"launchctl bootout gui/501 /tmp/com.clipport.app.plist",
		"cleanup /tmp/Clipport.app/Contents/MacOS/clipport,/tmp/bin/clipport",
		"launchctl bootstrap gui/501 /tmp/com.clipport.app.plist",
		"launchctl kickstart -k gui/501/" + uninstall.AppLaunchdLabel,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

func TestIsInstalledAppCommandRequiresExactExecutablePath(t *testing.T) {
	wanted := map[string]bool{
		"/tmp/Clipport.app/Contents/MacOS/clipport":             true,
		"/tmp/Clipport With Spaces.app/Contents/MacOS/clipport": true,
		"/tmp/bin/clipport": true,
	}
	for _, executable := range []string{
		"/tmp/Clipport.app/Contents/MacOS/clipport",
		"/tmp/Clipport With Spaces.app/Contents/MacOS/clipport",
		"/tmp/bin/clipport",
	} {
		if !isInstalledAppExecutable(executable, wanted) {
			t.Fatalf("executable %q did not match", executable)
		}
	}
	for _, executable := range []string{
		"/tmp/bin/clipportd",
		"/tmp/other/clipport paste",
		"",
	} {
		if isInstalledAppExecutable(executable, wanted) {
			t.Fatalf("executable %q should not match", executable)
		}
	}
}

func TestExecutablePathFromLsofHandlesSpaces(t *testing.T) {
	out := "p123\nftxt\nn/tmp/Clipport With Spaces.app/Contents/MacOS/clipport\n"
	got, ok := executablePathFromLsof(out)
	if !ok {
		t.Fatal("expected executable path")
	}
	want := "/tmp/Clipport With Spaces.app/Contents/MacOS/clipport"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestCleanupInstalledAppProcessesWaitsThenKillsStillRunningProcess(t *testing.T) {
	var signals []syscall.Signal
	cleanup := appProcessCleaner{
		listClipportPIDs: func() ([]int, error) { return []int{42}, nil },
		executablePath:   func(int) (string, error) { return "/tmp/bin/clipport", nil },
		signal: func(pid int, sig syscall.Signal) error {
			if pid != 42 {
				t.Fatalf("pid = %d", pid)
			}
			signals = append(signals, sig)
			return nil
		},
		isRunning:    func(int) bool { return true },
		sleep:        func(time.Duration) {},
		waitInterval: time.Millisecond,
		waitTimeout:  0,
	}

	if err := cleanup.clean([]string{"/tmp/bin/clipport"}); err != nil {
		t.Fatal(err)
	}
	want := []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}
	if len(signals) != len(want) {
		t.Fatalf("signals = %v, want %v", signals, want)
	}
	for i := range want {
		if signals[i] != want[i] {
			t.Fatalf("signals = %v, want %v", signals, want)
		}
	}
}

func TestMaybeRestartInstalledAppSkipsWhenLaunchAgentUnknown(t *testing.T) {
	called := false
	err := maybeRestartInstalledApp(
		func(string) (config.LocalConfig, error) {
			return config.LocalConfig{}, nil
		},
		"",
		func(name string, args ...string) error {
			called = true
			return nil
		},
		501,
	)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("did not expect launchctl kickstart")
	}
}

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

func runClipctlForTest(args ...string) (string, error) {
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func envContains(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}

func assertCommandUsage(t *testing.T, output string) {
	t.Helper()
	for _, want := range []string{
		"usage: clipctl [--socket path] [--debug] paste",
		"clipctl [--socket path] session register --machine name",
		"clipctl start [--config path]",
		"clipctl shims setup --host machine",
		"clipctl update [--config path]",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("usage output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Usage of") {
		t.Fatalf("usage output used Go flag defaults:\n%s", output)
	}
}
