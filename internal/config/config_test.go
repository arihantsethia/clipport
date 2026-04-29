package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadResolveHostAndSortRoutes(t *testing.T) {
	path := writeConfig(t, `
default_host = "devbox"

[[hosts]]
name = "devbox"
match_hosts = ["vm-devbox", "devbox.example.com"]

[[hosts.routes]]
name = "public"
ssh_target = "devbox-public"
priority = 20

[[hosts.routes]]
name = "lan"
ssh_target = "dev@192.0.2.10"
priority = 10
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	host, ok := cfg.ResolveHost("vm-devbox")
	if !ok {
		t.Fatal("expected vm-devbox to resolve")
	}
	if host.Name != "devbox" {
		t.Fatalf("host = %q, want devbox", host.Name)
	}
	routes := host.SortedRoutes()
	if routes[0].Name != "lan" || routes[1].Name != "public" {
		t.Fatalf("routes sorted as %v, want lan then public", routes)
	}
}

func TestValidateRejectsDuplicateHosts(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
name = "devbox"
[[hosts.routes]]
name = "lan"
ssh_target = "a"

[[hosts]]
name = "devbox"
[[hosts.routes]]
name = "lan"
ssh_target = "b"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected duplicate host error")
	}
}

func TestResolveUsesDefaultHost(t *testing.T) {
	path := writeConfig(t, `
default_host = "devbox"
[[hosts]]
name = "devbox"
[[hosts.routes]]
name = "public"
ssh_target = "devbox-public"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	host, ok := cfg.ResolveHost("")
	if !ok || host.Name != "devbox" {
		t.Fatalf("default resolution = (%q, %v), want devbox true", host.Name, ok)
	}
}

func TestLoadAllowsLocalSettingsWithoutHosts(t *testing.T) {
	path := writeConfig(t, `
[local]
bin_dir = "/tmp/bin"
http_addr = "127.0.0.1:18765"

[local.iterm]
key = "0x76-0x120000"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Local.BinDir != "/tmp/bin" || cfg.Local.HTTPAddr != "127.0.0.1:18765" || cfg.Local.Iterm.Key != "0x76-0x120000" {
		t.Fatalf("cfg = %+v", cfg)
	}
}
