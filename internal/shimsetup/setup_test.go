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

func TestSetupRejectsNonAliasRouteTarget(t *testing.T) {
	cfg := &config.Config{
		Hosts: []config.Host{
			{
				Name: "devbox",
				Routes: []config.Route{
					{Name: "lan", SSHTarget: "dev@192.0.2.10"},
				},
			},
		},
	}
	_, err := setup("/tmp/config.toml", "devbox", "/tmp/ssh_config", "/tmp/token", 18765, deps{
		loadConfig: func(path string) (*config.Config, error) {
			return cfg, nil
		},
		loadToken: func(path string) (string, error) {
			return "secret-token", nil
		},
		installForward: func(configPath, host string, remotePort int) (string, error) {
			t.Fatal("installForward should not run for non-alias route target")
			return "", nil
		},
		installShims: func(target, token string, port int) error {
			t.Fatal("installShims should not run for non-alias route target")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected non-alias route target error")
	}
	if !strings.Contains(err.Error(), `route "lan" target "dev@192.0.2.10" cannot be used for SSH config setup`) {
		t.Fatalf("unexpected error: %v", err)
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
			t.Fatal("loadToken should not run for unknown logical host")
			return "", nil
		},
		installForward: func(configPath, host string, remotePort int) (string, error) {
			t.Fatal("installForward should not run for unknown logical host")
			return "", nil
		},
		installShims: func(target, token string, port int) error {
			t.Fatal("installShims should not run for unknown logical host")
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
