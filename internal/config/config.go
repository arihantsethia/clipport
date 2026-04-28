package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DefaultHost string `toml:"default_host"`
	Hosts       []Host `toml:"hosts"`
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
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func LoadDefault() (*Config, error) {
	path := os.Getenv("CLIPPORT_CONFIG")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = home + "/.config/clipport/config.toml"
	}
	return Load(path)
}

func (c *Config) Validate() error {
	if len(c.Hosts) == 0 {
		return errors.New("config must define at least one host")
	}
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
