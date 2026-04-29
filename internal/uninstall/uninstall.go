package uninstall

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/sshsetup"
)

const AppLaunchdLabel = "com.clipport.app"

type Options struct {
	BinDir          string
	ConfigPath      string
	SSHConfig       string
	RemoveData      bool
	RemoveIterm     bool
	RemoveItermSet  bool
	ItermKey        string
	DryRun          bool
	CurrentExe      string
	AppLaunchdPlist string
	AppPath         string
}

type Result struct {
	Actions []string
}

func Run(opts Options) (Result, error) {
	opts, configPath, configLoaded, err := loadInstallChoices(opts)
	if err != nil {
		return Result{}, err
	}
	opts = withDefaults(opts)
	var result Result
	verb := "removed"
	if opts.DryRun {
		verb = "would remove"
	}
	record := func(format string, args ...any) {
		result.Actions = append(result.Actions, fmt.Sprintf(format, args...))
	}
	if opts.DryRun {
		record("dry run: no files changed")
	}
	if configLoaded {
		record("using config %s", configPath)
	} else if configPath != "" {
		record("no config found at %s; using defaults and explicit flags", configPath)
	}

	if opts.AppLaunchdPlist != "" {
		if err := bootout(opts.AppLaunchdPlist, opts.DryRun); err != nil {
			return result, err
		}
		record("stopped launchd service %s if it was running", AppLaunchdLabel)
		if err := removePath(opts.AppLaunchdPlist, opts.DryRun); err != nil {
			return result, err
		}
		record("%s app launchd agent %s", verb, opts.AppLaunchdPlist)
	}

	if opts.DryRun {
		record("would remove clipport SSH config blocks from %s", opts.SSHConfig)
	} else if backup, err := sshsetup.RemoveAllClipportBlocks(opts.SSHConfig); err == nil {
		record("removed clipport SSH config blocks; backup %s", backup)
	} else if errors.Is(err, sshsetup.ErrNoClipportBlocks) || os.IsNotExist(err) {
		record("no clipport SSH config blocks found")
	} else {
		return result, err
	}

	if opts.RemoveIterm {
		removed, err := removeItermHotkey(opts.ItermKey, opts.DryRun)
		if err != nil {
			return result, err
		}
		if removed {
			record("%s clipport iTerm hotkey %s", verb, opts.ItermKey)
		} else {
			record("no matching clipport iTerm hotkey found")
		}
	}

	for _, name := range []string{"clipctl", "clipportd", "clipport"} {
		path := filepath.Join(opts.BinDir, name)
		if err := removePath(path, opts.DryRun); err != nil {
			return result, err
		}
		record("%s %s", verb, path)
	}
	if opts.AppPath != "" {
		if err := removePath(opts.AppPath, opts.DryRun); err != nil {
			return result, err
		}
		record("%s app bundle %s", verb, opts.AppPath)
	}

	if opts.RemoveData {
		for _, path := range dataPaths(opts) {
			if err := removePath(path, opts.DryRun); err != nil {
				return result, err
			}
			record("%s %s", verb, path)
		}
	} else {
		record("kept config, cache, and token files; pass --remove-data to delete them")
	}
	return result, nil
}

func loadInstallChoices(opts Options) (Options, string, bool, error) {
	path := opts.ConfigPath
	if path == "" {
		path = config.DefaultPath()
	}
	if path == "" {
		return opts, "", false, nil
	}
	local, err := config.LoadLocalBestEffort(path)
	if err != nil {
		return opts, path, false, err
	}
	opts.ConfigPath = path
	if local == (config.LocalConfig{}) {
		if !opts.RemoveItermSet {
			opts.RemoveIterm = true
		}
		return opts, path, false, nil
	}
	return applyLocalSettings(opts, local), path, true, nil
}

func withDefaults(opts Options) Options {
	home, _ := os.UserHomeDir()
	if opts.ConfigPath == "" {
		opts.ConfigPath = config.DefaultPath()
	}
	if opts.CurrentExe == "" {
		opts.CurrentExe, _ = os.Executable()
	}
	if opts.BinDir == "" && opts.CurrentExe != "" {
		opts.BinDir = filepath.Dir(opts.CurrentExe)
	}
	if opts.BinDir == "" && home != "" {
		opts.BinDir = filepath.Join(home, ".local", "bin")
	}
	if opts.SSHConfig == "" {
		opts.SSHConfig = sshsetup.DefaultSSHConfigPath()
	}
	if opts.ItermKey == "" {
		opts.ItermKey = "0x76-0x120000"
	}
	if opts.AppLaunchdPlist == "" && home != "" {
		opts.AppLaunchdPlist = filepath.Join(home, "Library", "LaunchAgents", AppLaunchdLabel+".plist")
	}
	if opts.AppPath == "" && home != "" {
		opts.AppPath = filepath.Join(home, "Applications", "Clipport.app")
	}
	return opts
}

func dataPaths(opts Options) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{
		filepath.Join(home, ".config", "clipport"),
		filepath.Join(home, ".cache", "clipport"),
		filepath.Join(os.TempDir(), "clipport"),
	}
	if opts.ConfigPath != "" && !isUnder(opts.ConfigPath, filepath.Join(home, ".config", "clipport")) {
		paths = append(paths, opts.ConfigPath)
	}
	return paths
}

func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func bootout(plistPath string, dryRun bool) error {
	if dryRun {
		return nil
	}
	cmd := exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), plistPath)
	_ = cmd.Run()
	return nil
}

func removePath(path string, dryRun bool) error {
	if path == "" || dryRun {
		return nil
	}
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func removeItermHotkey(key string, dryRun bool) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	prefs := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	if _, err := os.Stat(prefs); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :GlobalKeyMap:"+key+":Text", prefs).CombinedOutput()
	if err != nil {
		return false, nil
	}
	if !strings.Contains(string(out), "clipctl paste") {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Delete :GlobalKeyMap:"+key, prefs).CombinedOutput(); err != nil {
		return false, fmt.Errorf("remove iTerm hotkey: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}
