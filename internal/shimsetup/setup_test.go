package shimsetup

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/sshsetup"
)

func TestSetupUsesLogicalHostAndSortedRoutes(t *testing.T) {
	cfg := &config.Config{
		Hosts: []config.Host{
			{
				Name:       "devbox",
				MatchHosts: []string{"vm-devbox"},
				Routes: []config.Route{
					{Name: "public", SSHTarget: "devbox-public", Priority: 20},
					{Name: "lan", SSHTarget: "devbox-lan", Priority: 10},
				},
			},
		},
	}
	var forwardTargets []string
	var shimTargets []string
	result, err := setup("/tmp/config.toml", "devbox", "/tmp/ssh_config", "/tmp/token", 18765, deps{
		loadConfig: func(path string) (*config.Config, error) {
			if path != "/tmp/config.toml" {
				t.Fatalf("loadConfig path = %q", path)
			}
			return cfg, nil
		},
		loadToken: func(path string) (string, error) {
			if path != "/tmp/token" {
				t.Fatalf("loadToken path = %q", path)
			}
			return "secret-token", nil
		},
		installForward: func(configPath, host string, remotePort int) (string, error) {
			if configPath != "/tmp/ssh_config" {
				t.Fatalf("installForward configPath = %q", configPath)
			}
			if remotePort != 18765 {
				t.Fatalf("installForward remotePort = %d", remotePort)
			}
			forwardTargets = append(forwardTargets, host)
			return "", nil
		},
		installShims: func(target, token string, port int) error {
			if token != "secret-token" {
				t.Fatalf("installShims token = %q", token)
			}
			if port != 18765 {
				t.Fatalf("installShims port = %d", port)
			}
			shimTargets = append(shimTargets, target)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Machine != "devbox" {
		t.Fatalf("result.Machine = %q, want devbox", result.Machine)
	}
	wantTargets := []string{"devbox-lan", "devbox-public"}
	if !reflect.DeepEqual(forwardTargets, wantTargets) {
		t.Fatalf("forward targets = %v, want %v", forwardTargets, wantTargets)
	}
	if !reflect.DeepEqual(shimTargets, wantTargets) {
		t.Fatalf("shim targets = %v, want %v", shimTargets, wantTargets)
	}
	wantRoutes := []RouteResult{
		{Name: "lan", Target: "devbox-lan", ForwardStatus: "installed"},
		{Name: "public", Target: "devbox-public", ForwardStatus: "installed"},
	}
	if !reflect.DeepEqual(result.Routes, wantRoutes) {
		t.Fatalf("result.Routes = %#v, want %#v", result.Routes, wantRoutes)
	}
}

func TestSetupTreatsExistingForwardAsSuccess(t *testing.T) {
	cfg := &config.Config{
		Hosts: []config.Host{
			{
				Name: "devbox",
				Routes: []config.Route{
					{Name: "public", SSHTarget: "devbox-public"},
				},
			},
		},
	}
	calledShims := false
	result, err := setup("/tmp/config.toml", "devbox", "/tmp/ssh_config", "/tmp/token", 18765, deps{
		loadConfig: func(path string) (*config.Config, error) {
			return cfg, nil
		},
		loadToken: func(path string) (string, error) {
			return "secret-token", nil
		},
		installForward: func(configPath, host string, remotePort int) (string, error) {
			return "", fmt.Errorf("%w for %s", sshsetup.ErrForwardAlreadyInstalled, host)
		},
		installShims: func(target, token string, port int) error {
			calledShims = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !calledShims {
		t.Fatal("expected shims install to run")
	}
	if len(result.Routes) != 1 || result.Routes[0].ForwardStatus != "already present" {
		t.Fatalf("result.Routes = %#v", result.Routes)
	}
}

func TestSetupRequiresLogicalHostName(t *testing.T) {
	cfg := &config.Config{
		Hosts: []config.Host{
			{
				Name:       "devbox",
				MatchHosts: []string{"vm-devbox"},
				Routes: []config.Route{
					{Name: "public", SSHTarget: "devbox-public"},
				},
			},
		},
	}
	_, err := setup("/tmp/config.toml", "vm-devbox", "/tmp/ssh_config", "/tmp/token", 18765, deps{
		loadConfig: func(path string) (*config.Config, error) {
			return cfg, nil
		},
		loadToken: func(path string) (string, error) {
			t.Fatal("loadToken ran for unknown logical host")
			return "", nil
		},
		installForward: func(configPath, host string, remotePort int) (string, error) {
			t.Fatal("installForward ran for unknown logical host")
			return "", nil
		},
		installShims: func(target, token string, port int) error {
			t.Fatal("installShims ran for unknown logical host")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected host lookup error")
	}
	if want := `host "vm-devbox" not found in config`; err.Error() != want {
		t.Fatalf("err = %q, want %q", err, want)
	}
}

func TestSetupRefusesConflictBeforeChangingForward(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{Name: "devbox", Routes: []config.Route{{Name: "lan", SSHTarget: "devbox-lan"}}}}}
	_, err := setup("", "devbox", "", "", 18765, deps{
		loadConfig:   func(string) (*config.Config, error) { return cfg, nil },
		loadToken:    func(string) (string, error) { return "client_B", nil },
		installShims: func(string, string, int) error { return fmt.Errorf("remote token differs") },
		installForward: func(string, string, int) (string, error) {
			t.Fatal("forward changed before token conflict was resolved")
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "local forwarding was not changed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetupValidatesEveryAliasBeforeAnyInstallation(t *testing.T) {
	for _, target := range []string{"user@host", "dev@192.0.2.10"} {
		t.Run(target, func(t *testing.T) {
			cfg := &config.Config{Hosts: []config.Host{{Name: "devbox", Routes: []config.Route{{Name: "first", SSHTarget: "devbox-lan", Priority: 1}, {Name: "bad", SSHTarget: target, Priority: 2}}}}}
			_, err := setup("", "devbox", "", "", 18765, deps{
				loadConfig:   func(string) (*config.Config, error) { return cfg, nil },
				loadToken:    func(string) (string, error) { t.Fatal("created token before validating routes"); return "", nil },
				installShims: func(string, string, int) error { t.Fatal("installed before validating all routes"); return nil },
				installForward: func(string, string, int) (string, error) {
					t.Fatal("changed SSH config before validating all routes")
					return "", nil
				},
			})
			want := fmt.Sprintf(`route "bad" target %q cannot be used for SSH config setup`, target)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("unexpected invalid alias failure: %v", err)
			}
		})
	}
}

func TestSetupReportsPartialRouteFailure(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{Name: "devbox", Routes: []config.Route{{Name: "first", SSHTarget: "first", Priority: 1}, {Name: "second", SSHTarget: "second", Priority: 2}}}}}
	var calls []string
	result, err := setup("", "devbox", "", "", 18765, deps{
		loadConfig: func(string) (*config.Config, error) { return cfg, nil },
		loadToken:  func(string) (string, error) { return "client_A", nil },
		installShims: func(target, token string, port int) error {
			calls = append(calls, "shim:"+target)
			if target == "second" {
				return fmt.Errorf("conflict")
			}
			return nil
		},
		installForward: func(path, target string, port int) (string, error) {
			calls = append(calls, "forward:"+target)
			return "", nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "after 1 completed route(s)") {
		t.Fatalf("partial completion missing: %v", err)
	}
	if len(result.Routes) != 1 || !reflect.DeepEqual(calls, []string{"shim:first", "forward:first", "shim:second"}) {
		t.Fatalf("unexpected operations: %v, result: %#v", calls, result)
	}
}

func TestSetupRejectsInvalidPortBeforeLoadingState(t *testing.T) {
	for _, port := range []int{-1, 65536} {
		_, err := setup("", "devbox", "", "", port, deps{})
		if err == nil || !strings.Contains(err.Error(), "port must") {
			t.Fatalf("invalid port accepted: %v", err)
		}
	}
}

func TestSetupReportsForwardFailureAfterRemoteInstallation(t *testing.T) {
	cfg := &config.Config{Hosts: []config.Host{{Name: "devbox", Routes: []config.Route{{Name: "lan", SSHTarget: "devbox-lan"}}}}}
	installed := false
	_, err := setup("", "devbox", "", "", 18765, deps{
		loadConfig:   func(string) (*config.Config, error) { return cfg, nil },
		loadToken:    func(string) (string, error) { return "client_A", nil },
		installShims: func(string, string, int) error { installed = true; return nil },
		installForward: func(string, string, int) (string, error) {
			if !installed {
				t.Fatal("forward installed before remote validation")
			}
			return "", fmt.Errorf("write failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "after remote shims were installed") {
		t.Fatalf("partial remote change missing: %v", err)
	}
}
