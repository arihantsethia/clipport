package onboard

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
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

func TestReadSSHConfigReportsBadIncludePattern(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Include [\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSSHConfig(path)
	if err == nil {
		t.Fatal("expected bad include pattern error")
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

func TestBuildConfigSortsMatchHosts(t *testing.T) {
	group, err := ParseHostGroup("devbox=devbox-public,devbox-lan")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := BuildConfig([]HostGroup{group}, []SSHHost{
		{Alias: "devbox-public", HostName: "z.example.com"},
		{Alias: "devbox-lan", HostName: "a.local"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(cfg.Hosts[0].MatchHosts) {
		t.Fatalf("match hosts are not sorted: %v", cfg.Hosts[0].MatchHosts)
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

func TestTUISelectViewExplainsHowToFinish(t *testing.T) {
	m := NewTUIModel([]SSHHost{
		{Alias: "devbox-lan", HostName: "192.0.2.10", User: "dev"},
	})
	m.input = "devbox"
	m.step = stepSelect

	view := m.viewSelect()

	for _, want := range []string{
		"enter save machine",
		"d review and finish",
		"After saving, add another machine or review and write the config.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestTUINameViewExplainsHowToExitLoopAfterFirstMachine(t *testing.T) {
	m := NewTUIModel([]SSHHost{
		{Alias: "devbox-lan", HostName: "192.0.2.10", User: "dev"},
	})
	m.groups = []HostGroup{{Name: "devbox", Routes: []string{"devbox-lan"}}}
	m.step = stepName

	view := m.viewName()

	for _, want := range []string{
		"Press esc to review the current machines and write the config.",
		"esc review and finish",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestMaybeConfigureItermAcceptsDefaultYes(t *testing.T) {
	var called bool
	var out bytes.Buffer
	ok, err := MaybeConfigureIterm("0x76-0x120000", "/bin/clipctl", strings.NewReader("\n"), &out, func(key, command string) error {
		called = true
		if key != "0x76-0x120000" || command != "/bin/clipctl paste" {
			t.Fatalf("unexpected args: %q %q", key, command)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !called {
		t.Fatalf("ok=%v called=%v", ok, called)
	}
}

func TestMaybeConfigureItermSkipsOnEmptyEOF(t *testing.T) {
	var called bool
	ok, err := MaybeConfigureIterm("0x76-0x120000", "/bin/clipctl", strings.NewReader(""), &bytes.Buffer{}, func(key, command string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok || called {
		t.Fatalf("ok=%v called=%v", ok, called)
	}
}

func TestWriteConfigPreservesLocalSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[local]
bin_dir = "/tmp/bin"
app_launchd_plist_path = "/tmp/com.clipport.app.plist"
http_addr = "127.0.0.1:18765"

[local.iterm]
key = "0x76-0x120000"
configured = true
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultHost: "devbox",
		Hosts: []config.Host{{
			Name: "devbox",
			Routes: []config.Route{{
				Name:      "lan",
				SSHTarget: "devbox-lan",
				Priority:  10,
			}},
		}},
	}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Local.BinDir != "/tmp/bin" || loaded.Local.AppLaunchdPlistPath != "/tmp/com.clipport.app.plist" || loaded.Local.HTTPAddr != "127.0.0.1:18765" {
		t.Fatalf("local settings lost: %+v", loaded)
	}
	if !loaded.Local.Iterm.Configured || loaded.Local.Iterm.Key != "0x76-0x120000" {
		t.Fatalf("iterm settings lost: %+v", loaded)
	}
	if len(loaded.Hosts) != 1 || loaded.Hosts[0].Name != "devbox" {
		t.Fatalf("hosts = %+v", loaded.Hosts)
	}
}

func TestWriteConfigPreservesLocalSettingsFromInvalidHostConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[local]
bin_dir = "/tmp/bin"

[local.iterm]
key = "0x76-0x120000"

[[hosts]]
name = "broken"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultHost: "devbox",
		Hosts: []config.Host{{
			Name: "devbox",
			Routes: []config.Route{{
				Name:      "lan",
				SSHTarget: "devbox-lan",
				Priority:  10,
			}},
		}},
	}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Local.BinDir != "/tmp/bin" || loaded.Local.Iterm.Key != "0x76-0x120000" {
		t.Fatalf("local settings lost: %+v", loaded.Local)
	}
}

func TestWriteConfigOverwritesSyntaxErrorConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[local\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultHost: "devbox",
		Hosts: []config.Host{{
			Name: "devbox",
			Routes: []config.Route{{
				Name:      "lan",
				SSHTarget: "devbox-lan",
				Priority:  10,
			}},
		}},
	}
	if err := WriteConfig(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Hosts) != 1 || loaded.Hosts[0].Name != "devbox" {
		t.Fatalf("hosts = %+v", loaded.Hosts)
	}
}
