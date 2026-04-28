package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Registry struct {
	Hosts map[string]HostState `json:"hosts"`
}

type HostState struct {
	LastHealthyRoute string `json:"last_healthy_route,omitempty"`
	LastPastePath    string `json:"last_paste_path,omitempty"`
	LastPasteLatency string `json:"last_paste_latency,omitempty"`
	LastPasteAt      string `json:"last_paste_at,omitempty"`
	ShimVersion      string `json:"shim_version,omitempty"`
	ForwardInstalled bool   `json:"forward_installed,omitempty"`
}

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "clipport", "hosts.json")
}

func Load(path string) (*Registry, error) {
	if path == "" {
		path = DefaultPath()
	}
	r := &Registry{Hosts: map[string]HostState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, r); err != nil {
		return nil, err
	}
	if r.Hosts == nil {
		r.Hosts = map[string]HostState{}
	}
	return r, nil
}

func (r *Registry) Save(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (r *Registry) UpdateHost(name string, update func(HostState) HostState) {
	if r.Hosts == nil {
		r.Hosts = map[string]HostState{}
	}
	r.Hosts[name] = update(r.Hosts[name])
}
