package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DefaultHost string      `toml:"default_host"`
	Hosts       []Host      `toml:"hosts"`
	Local       LocalConfig `toml:"local"`
}

type LocalConfig struct {
	BinDir              string      `toml:"bin_dir,omitempty"`
	SSHConfigPath       string      `toml:"ssh_config_path,omitempty"`
	AppLaunchdPlistPath string      `toml:"app_launchd_plist_path,omitempty"`
	AppPath             string      `toml:"app_path,omitempty"`
	HTTPAddr            string      `toml:"http_addr,omitempty"`
	Iterm               ItermConfig `toml:"iterm"`
}

type ItermConfig struct {
	Key        string `toml:"key,omitempty"`
	Configured bool   `toml:"configured,omitempty"`
}

type localOnly struct {
	Local LocalConfig `toml:"local"`
}

type Host struct {
	Name       string   `toml:"name"`
	MatchHosts []string `toml:"match_hosts"`
	Routes     []Route  `toml:"routes"`
}

type Route struct {
	Name      string `toml:"name"`
	SSHTarget string `toml:"ssh_target"`
	Priority  int    `toml:"priority"`
}

func Load(path string) (*Config, error) {
	return load(path, true)
}

func LoadUnvalidated(path string) (*Config, error) {
	return load(path, false)
}

func load(path string, validate bool) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	if validate {
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

func LoadLocal(path string) (LocalConfig, error) {
	if path == "" {
		path = DefaultPath()
	}
	var cfg localOnly
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return LocalConfig{}, err
	}
	return cfg.Local, nil
}

func LoadLocalBestEffort(path string) (LocalConfig, error) {
	local, err := LoadLocal(path)
	if err == nil {
		return local, nil
	}
	if os.IsNotExist(err) || isParseError(err) {
		return LocalConfig{}, nil
	}
	return LocalConfig{}, err
}

func isParseError(err error) bool {
	var parseErr toml.ParseError
	if errors.As(err, &parseErr) {
		return true
	}
	return false
}

func LoadUnvalidatedOrEmpty(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	cfg, err := LoadUnvalidated(path)
	if err == nil {
		return cfg, nil
	}
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	return nil, err
}

func LoadDefault() (*Config, error) {
	path := DefaultPath()
	return Load(path)
}

func DefaultPath() string {
	path := os.Getenv("CLIPPORT_CONFIG")
	if path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "clipport", "config.toml")
}

func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return toml.NewEncoder(file).Encode(c)
}

func (c *Config) Validate() error {
	seen := map[string]bool{}
	for _, h := range c.Hosts {
		if h.Name == "" {
			return errors.New("host name is required")
		}
		if seen[h.Name] {
			return fmt.Errorf("duplicate host %q", h.Name)
		}
		seen[h.Name] = true
		if len(h.Routes) == 0 {
			return fmt.Errorf("host %q must define at least one route", h.Name)
		}
		for _, r := range h.Routes {
			if r.Name == "" {
				return fmt.Errorf("host %q has a route without a name", h.Name)
			}
			if r.SSHTarget == "" {
				return fmt.Errorf("host %q route %q must define ssh_target", h.Name, r.Name)
			}
		}
	}
	if c.DefaultHost != "" {
		if _, ok := c.HostByName(c.DefaultHost); !ok {
			return fmt.Errorf("default_host %q does not match any host", c.DefaultHost)
		}
	}
	return nil
}

func (c *Config) ValidateHostsRequired() error {
	if len(c.Hosts) == 0 {
		return errors.New("config must define at least one host")
	}
	return c.Validate()
}

func (c *Config) HostByName(name string) (Host, bool) {
	for _, h := range c.Hosts {
		if h.Name == name {
			return h, true
		}
	}
	return Host{}, false
}

func (c *Config) ResolveHost(detected string) (Host, bool) {
	detected = strings.TrimSpace(detected)
	for _, h := range c.Hosts {
		if h.Name == detected {
			return h, true
		}
		for _, alias := range h.MatchHosts {
			if alias == detected {
				return h, true
			}
		}
	}
	if c.DefaultHost != "" {
		return c.HostByName(c.DefaultHost)
	}
	return Host{}, false
}

func (h Host) SortedRoutes() []Route {
	routes := append([]Route(nil), h.Routes...)
	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].Priority < routes[j].Priority
	})
	return routes
}
