package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/uninstall"
)

func defaultItermKey(configPath string) string {
	if local, err := config.LoadLocalBestEffort(configPath); err == nil && local.Iterm.Key != "" {
		return local.Iterm.Key
	}
	return "0x76-0x120000"
}

func configureItermHotkey(key, commandText string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	prefs := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	if err := os.MkdirAll(filepath.Dir(prefs), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(prefs); os.IsNotExist(err) {
		if err := os.WriteFile(prefs, []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict/></plist>\n"), 0o600); err != nil {
			return err
		}
	}
	_ = exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap dict", prefs).Run()
	_ = exec.Command("/usr/libexec/PlistBuddy", "-c", "Delete :GlobalKeyMap:"+key, prefs).Run()
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap:"+key+" dict", prefs).CombinedOutput(); err != nil {
		return fmt.Errorf("configure iTerm hotkey: %w: %s", err, string(out))
	}
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap:"+key+":Action integer 35", prefs).CombinedOutput(); err != nil {
		return fmt.Errorf("configure iTerm hotkey: %w: %s", err, string(out))
	}
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Add :GlobalKeyMap:"+key+":Text string "+commandText, prefs).CombinedOutput(); err != nil {
		return fmt.Errorf("configure iTerm hotkey: %w: %s", err, string(out))
	}
	return nil
}

func recordItermConfigured(configPath, key string) error {
	cfg, err := config.LoadUnvalidatedOrEmpty(configPath)
	if err != nil {
		return err
	}
	cfg.Local.Iterm.Configured = true
	if cfg.Local.Iterm.Key == "" {
		cfg.Local.Iterm.Key = key
	}
	return cfg.Save(configPath)
}

func recordInstallSettings(configPath, binDir, sshConfigPath, appLaunchdPlistPath, appPath, httpAddr, itermKey string) error {
	if httpAddr != "" && !isLoopbackHTTPAddr(httpAddr) {
		return fmt.Errorf("http address must bind to loopback, got %s", httpAddr)
	}
	cfg, err := config.LoadUnvalidatedOrEmpty(configPath)
	if err != nil {
		return err
	}
	if binDir != "" {
		cfg.Local.BinDir = binDir
	}
	if sshConfigPath != "" {
		cfg.Local.SSHConfigPath = sshConfigPath
	}
	if appLaunchdPlistPath != "" {
		cfg.Local.AppLaunchdPlistPath = appLaunchdPlistPath
	}
	if appPath != "" {
		cfg.Local.AppPath = appPath
	}
	if httpAddr != "" {
		cfg.Local.HTTPAddr = httpAddr
	}
	if itermKey != "" {
		if cfg.Local.Iterm.Key != "" && cfg.Local.Iterm.Key != itermKey {
			cfg.Local.Iterm.Configured = false
		}
		cfg.Local.Iterm.Key = itermKey
	}
	return cfg.Save(configPath)
}

func prepareHomebrewInstallForOnboard(configPath, sshConfigPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	prefix, ok := homebrewPrefixForExecutable(exe)
	if !ok {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return recordHomebrewInstall(configPath, sshConfigPath, prefix, home)
}

func recordHomebrewInstall(configPath, sshConfigPath, prefix, home string) error {
	appLink, err := ensureUserAppLink(prefix, home)
	if err != nil {
		return err
	}
	httpAddr, err := missingHTTPAddr(configPath)
	if err != nil {
		return err
	}
	return recordInstallSettings(
		configPath,
		filepath.Join(prefix, "bin"),
		sshConfigPath,
		filepath.Join(prefix, "libexec", uninstall.AppLaunchdLabel+".plist"),
		appLink,
		httpAddr,
		"0x76-0x120000",
	)
}

func homebrewPrefixForExecutable(exe string) (string, bool) {
	resolvedExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolvedExe = exe
	}
	for _, prefix := range homebrewPrefixCandidates() {
		if !homebrewClipportFilesExist(prefix) {
			continue
		}
		clipctlPath := filepath.Join(prefix, "bin", "clipctl")
		resolvedClipctl, err := filepath.EvalSymlinks(clipctlPath)
		if err != nil {
			resolvedClipctl = clipctlPath
		}
		if resolvedExe == resolvedClipctl || exe == clipctlPath {
			return prefix, true
		}
	}
	return "", false
}

func homebrewPrefixCandidates() []string {
	seen := map[string]bool{}
	var prefixes []string
	for _, prefix := range []string{
		filepath.Join(os.Getenv("HOMEBREW_PREFIX"), "opt", "clipport"),
		"/opt/homebrew/opt/clipport",
		"/usr/local/opt/clipport",
	} {
		if prefix == "opt/clipport" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func homebrewClipportFilesExist(prefix string) bool {
	for _, path := range []string{
		filepath.Join(prefix, "bin", "clipctl"),
		filepath.Join(prefix, "libexec", "Clipport.app", "Contents", "MacOS", "clipport"),
		filepath.Join(prefix, "libexec", uninstall.AppLaunchdLabel+".plist"),
	} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func ensureUserAppLink(prefix, home string) (string, error) {
	appTarget := filepath.Join(prefix, "libexec", "Clipport.app")
	appLink := filepath.Join(home, "Applications", "Clipport.app")
	if _, err := os.Stat(appTarget); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(appLink), 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(appLink)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(appLink); err != nil {
				return "", err
			}
		} else if existingBundleID(appLink) == "com.clipport.app" {
			if err := os.RemoveAll(appLink); err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("%s already exists and is not Clipport", appLink)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(appTarget, appLink); err != nil {
		return "", err
	}
	registerAppWithLaunchServices(appLink)
	return appLink, nil
}

func existingBundleID(appPath string) string {
	data, err := os.ReadFile(filepath.Join(appPath, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	if strings.Contains(string(data), "<string>com.clipport.app</string>") {
		return "com.clipport.app"
	}
	return ""
}

func registerAppWithLaunchServices(appPath string) {
	lsregister := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	if _, err := os.Stat(lsregister); err != nil {
		return
	}
	_ = exec.Command(lsregister, "-f", appPath).Run()
}

func missingHTTPAddr(configPath string) (string, error) {
	local, err := config.LoadLocalBestEffort(configPath)
	if err != nil {
		return "", err
	}
	if local.HTTPAddr != "" {
		return "", nil
	}
	return freeLoopbackHTTPAddr(18765, 18865)
}

func freeLoopbackHTTPAddr(start, end int) (string, error) {
	for port := start; port <= end; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			_ = listener.Close()
			return addr, nil
		}
	}
	return "", fmt.Errorf("no free loopback HTTP port found in %d-%d", start, end)
}

func isLoopbackHTTPAddr(addr string) bool {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if _, err := strconv.Atoi(portText); err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolvedLocalSettings(loadLocal func(string) (config.LocalConfig, error), configPath string) (config.LocalConfig, error) {
	local, err := loadLocal(configPath)
	if err != nil {
		return config.LocalConfig{}, err
	}
	if local.BinDir == "" {
		local.BinDir = defaultBinDir()
	}
	if local.AppLaunchdPlistPath == "" {
		local.AppLaunchdPlistPath = defaultAppLaunchdPlistPath()
	}
	if local.AppPath == "" {
		local.AppPath = defaultAppPath()
	}
	return local, nil
}

func defaultBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}

func defaultAppLaunchdPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", uninstall.AppLaunchdLabel+".plist")
}

func defaultAppPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Applications", "Clipport.app")
}

func updateClipport(configPath string, stdout, stderr io.Writer) error {
	local, err := resolvedLocalSettings(config.LoadLocalBestEffort, configPath)
	if err != nil {
		return err
	}
	name, args, env := buildUpdateInvocation(configPath, local)
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func buildUpdateInvocation(configPath string, local config.LocalConfig) (string, []string, []string) {
	env := append([]string{}, os.Environ()...)
	if local.BinDir != "" {
		env = append(env, "CLIPPORT_BIN="+local.BinDir)
	}
	if configPath != "" {
		env = append(env, "CLIPPORT_CONFIG="+configPath)
	}
	if local.HTTPAddr != "" {
		env = append(env, "CLIPPORT_HTTP="+local.HTTPAddr)
	}
	if local.Iterm.Key != "" {
		env = append(env, "CLIPPORT_ITERM_KEY="+local.Iterm.Key)
	}
	return "sh", []string{"-c", curlInstallerScript()}, env
}

func curlInstallerScript() string {
	return `set -eu
repo_ref="${CLIPPORT_REF:-main}"
curl -fsSL "https://raw.githubusercontent.com/arihantsethia/clipport/${repo_ref}/install.sh" | sh`
}
