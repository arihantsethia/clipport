package onboard

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReadSSHConfigIncludesAliases(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "extra")
	if err := os.WriteFile(extra, []byte("Host devbox-lan\n  HostName 192.0.2.10\n  User dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	if err := os.WriteFile(main, []byte("Include extra\nHost devbox-public\n  HostName devbox.example.com\n  User dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hosts, err := ReadSSHConfig(main)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %#v", hosts)
	}
}

func TestBuildConfigGroupsRoutes(t *testing.T) {
	group, err := ParseHostGroup("devbox=devbox-lan,devbox-public")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig([]HostGroup{group}, []SSHHost{
		{Alias: "devbox-lan", HostName: "192.0.2.10"},
		{Alias: "devbox-public", HostName: "devbox.example.com"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultHost != "devbox" || len(cfg.Hosts) != 1 || len(cfg.Hosts[0].Routes) != 2 {
		t.Fatalf("cfg = %#v", cfg)
	}
	if cfg.Hosts[0].Routes[0].SSHTarget != "devbox-lan" {
		t.Fatalf("first route = %#v", cfg.Hosts[0].Routes[0])
	}
}

func TestTUIModelSelectsAndNamesHost(t *testing.T) {
	m := NewTUIModel([]SSHHost{
		{Alias: "devbox-lan", HostName: "192.0.2.10", User: "dev"},
		{Alias: "devbox-public", HostName: "devbox.example.com", User: "dev"},
	})
	for _, r := range "devbox" {
		model, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = model.(TUIModel)
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(TUIModel)
	if m.step != stepSelect {
		t.Fatalf("step = %v, want stepSelect", m.step)
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = model.(TUIModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = model.(TUIModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = model.(TUIModel)
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(TUIModel)
	if len(m.groups) != 1 || m.groups[0].Name != "devbox" || len(m.groups[0].Routes) != 2 {
		t.Fatalf("groups = %#v", m.groups)
	}
}
