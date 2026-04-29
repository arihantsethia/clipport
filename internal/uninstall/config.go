package uninstall

import "github.com/arihantsethia/clipport/internal/config"

func applyLocalSettings(opts Options, local config.LocalConfig) Options {
	if opts.BinDir == "" {
		opts.BinDir = local.BinDir
	}
	if opts.SSHConfig == "" {
		opts.SSHConfig = local.SSHConfigPath
	}
	if opts.AppLaunchdPlist == "" {
		opts.AppLaunchdPlist = local.AppLaunchdPlistPath
	}
	if opts.AppPath == "" {
		opts.AppPath = local.AppPath
	}
	if opts.ItermKey == "" {
		opts.ItermKey = local.Iterm.Key
	}
	if !opts.RemoveItermSet {
		opts.RemoveIterm = local.Iterm.Configured
	}
	return opts
}
