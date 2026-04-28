package uninstall

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Manifest struct {
	BinDir                 string `toml:"bin_dir"`
	ConfigPath             string `toml:"config_path"`
	SSHConfigPath          string `toml:"ssh_config_path"`
	LaunchdPlistPath       string `toml:"launchd_plist_path"`
	HTTPAddr               string `toml:"http_addr"`
	ItermKey               string `toml:"iterm_key"`
	ItermConfigured        bool   `toml:"iterm_configured"`
	SessionHooksConfigured bool   `toml:"session_hooks_configured"`
}

func DefaultManifestPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clipport", "install.toml")
}

func LoadManifest(path string) (Manifest, error) {
	if path == "" {
		path = DefaultManifestPath()
	}
	var manifest Manifest
	if _, err := toml.DecodeFile(path, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func applyManifest(opts Options, manifest Manifest) Options {
	if opts.BinDir == "" {
		opts.BinDir = manifest.BinDir
	}
	if opts.ConfigPath == "" {
		opts.ConfigPath = manifest.ConfigPath
	}
	if opts.SSHConfig == "" {
		opts.SSHConfig = manifest.SSHConfigPath
	}
	if opts.LaunchdPlist == "" {
		opts.LaunchdPlist = manifest.LaunchdPlistPath
	}
	if opts.ItermKey == "" {
		opts.ItermKey = manifest.ItermKey
	}
	if !opts.RemoveItermSet {
		opts.RemoveIterm = manifest.ItermConfigured
	}
	return opts
}
