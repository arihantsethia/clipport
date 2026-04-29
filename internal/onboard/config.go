package onboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/arihantsethia/clipport/internal/config"
)

type HostGroup struct {
	Name   string
	Routes []string
}

func ParseHostGroup(s string) (HostGroup, error) {
	name, rest, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(rest) == "" {
		return HostGroup{}, fmt.Errorf("host group must look like name=route1,route2")
	}
	var routes []string
	for _, route := range strings.Split(rest, ",") {
		route = strings.TrimSpace(route)
		if route != "" {
			routes = append(routes, route)
		}
	}
	if len(routes) == 0 {
		return HostGroup{}, fmt.Errorf("host group %q has no routes", name)
	}
	return HostGroup{Name: strings.TrimSpace(name), Routes: routes}, nil
}

func BuildConfig(groups []HostGroup, sshHosts []SSHHost, defaultHost string) (*config.Config, error) {
	byAlias := map[string]SSHHost{}
	for _, h := range sshHosts {
		byAlias[h.Alias] = h
	}
	cfg := &config.Config{DefaultHost: defaultHost}
	for _, group := range groups {
		host := config.Host{Name: group.Name}
		aliases := map[string]bool{group.Name: true}
		for i, routeAlias := range group.Routes {
			sshHost, ok := byAlias[routeAlias]
			if !ok {
				return nil, fmt.Errorf("ssh host %q not found", routeAlias)
			}
			host.Routes = append(host.Routes, config.Route{
				Name:      routeAlias,
				SSHTarget: routeAlias,
				Priority:  (i + 1) * 10,
			})
			aliases[routeAlias] = true
			if sshHost.HostName != "" {
				aliases[sshHost.HostName] = true
			}
		}
		for alias := range aliases {
			host.MatchHosts = append(host.MatchHosts, alias)
		}
		sort.Strings(host.MatchHosts)
		cfg.Hosts = append(cfg.Hosts, host)
	}
	if cfg.DefaultHost == "" && len(cfg.Hosts) > 0 {
		cfg.DefaultHost = cfg.Hosts[0].Name
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func WriteConfig(path string, cfg *config.Config) error {
	existing, err := config.LoadLocalBestEffort(path)
	if err == nil {
		cfg.Local = existing
	}
	return cfg.Save(path)
}
