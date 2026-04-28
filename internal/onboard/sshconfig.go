package onboard

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SSHHost struct {
	Alias    string
	HostName string
	User     string
}

func ReadSSHConfig(path string) ([]SSHHost, error) {
	seen := map[string]SSHHost{}
	if err := readSSHConfig(path, seen, 0); err != nil {
		return nil, err
	}
	hosts := make([]SSHHost, 0, len(seen))
	for _, h := range seen {
		if strings.ContainsAny(h.Alias, "*?") {
			continue
		}
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Alias < hosts[j].Alias })
	return hosts, nil
}

func readSSHConfig(path string, seen map[string]SSHHost, depth int) error {
	if depth > 8 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dir := filepath.Dir(path)
	var current []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.ToLower(fields[0])
		values := fields[1:]
		switch key {
		case "include":
			for _, pattern := range values {
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(dir, pattern)
				}
				matches, _ := filepath.Glob(pattern)
				for _, match := range matches {
					_ = readSSHConfig(match, seen, depth+1)
				}
			}
		case "host":
			current = values
			for _, alias := range current {
				if _, ok := seen[alias]; !ok {
					seen[alias] = SSHHost{Alias: alias}
				}
			}
		case "hostname":
			for _, alias := range current {
				h := seen[alias]
				if h.HostName == "" {
					h.HostName = values[0]
				}
				seen[alias] = h
			}
		case "user":
			for _, alias := range current {
				h := seen[alias]
				if h.User == "" {
					h.User = values[0]
				}
				seen[alias] = h
			}
		}
	}
	return scanner.Err()
}
