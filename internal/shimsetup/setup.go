package shimsetup

import (
	"errors"
	"fmt"
	"strings"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/shims"
	"github.com/arihantsethia/clipport/internal/sshsetup"
	"github.com/arihantsethia/clipport/internal/token"
)

type Result struct {
	Machine string
	Routes  []RouteResult
}

type RouteResult struct {
	Name          string
	Target        string
	ForwardStatus string
}

type deps struct {
	loadConfig     func(path string) (*config.Config, error)
	loadToken      func(path string) (string, error)
	installForward func(configPath, host string, remotePort int) (string, error)
	installShims   func(target, token string, port int) error
	removeForward  func(configPath, host string) (string, error)
	uninstallShims func(target string, removeToken bool) error
}

func Setup(configPath, machine, sshConfigPath, tokenPath string, port int) (Result, error) {
	return setup(configPath, machine, sshConfigPath, tokenPath, port, deps{
		loadConfig:     config.Load,
		loadToken:      token.LoadOrCreate,
		installForward: sshsetup.InstallForward,
		installShims:   shims.Install,
		removeForward:  sshsetup.RemoveForward,
		uninstallShims: shims.Uninstall,
	})
}

func Uninstall(configPath, machine, sshConfigPath string, removeRemoteToken bool) (Result, error) {
	return uninstall(configPath, machine, sshConfigPath, removeRemoteToken, deps{
		loadConfig:     config.Load,
		removeForward:  sshsetup.RemoveForward,
		uninstallShims: shims.Uninstall,
	})
}

func setup(configPath, machine, sshConfigPath, tokenPath string, port int, d deps) (Result, error) {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return Result{}, fmt.Errorf("machine is required")
	}
	cfg, err := d.loadConfig(configPath)
	if err != nil {
		return Result{}, err
	}
	host, ok := cfg.HostByName(machine)
	if !ok {
		return Result{}, fmt.Errorf("host %q not found in config", machine)
	}
	bearer, err := d.loadToken(tokenPath)
	if err != nil {
		return Result{}, err
	}
	result := Result{Machine: host.Name}
	for _, route := range host.SortedRoutes() {
		routeResult := RouteResult{
			Name:   route.Name,
			Target: route.SSHTarget,
		}
		if err := sshsetup.ValidateHostAlias(route.SSHTarget); err != nil {
			return result, fmt.Errorf("route %q target %q cannot be used for SSH config setup; use an SSH config alias as ssh_target: %w", route.Name, route.SSHTarget, err)
		}
		_, err := d.installForward(sshConfigPath, route.SSHTarget, port)
		switch {
		case err == nil:
			routeResult.ForwardStatus = "installed"
		case errors.Is(err, sshsetup.ErrForwardAlreadyInstalled):
			routeResult.ForwardStatus = "already present"
		default:
			return result, fmt.Errorf("install forward for route %q (%s): %w", route.Name, route.SSHTarget, err)
		}
		if err := d.installShims(route.SSHTarget, bearer, port); err != nil {
			return result, fmt.Errorf("install shims for route %q (%s): %w", route.Name, route.SSHTarget, err)
		}
		result.Routes = append(result.Routes, routeResult)
	}
	return result, nil
}

func uninstall(configPath, machine, sshConfigPath string, removeRemoteToken bool, d deps) (Result, error) {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return Result{}, fmt.Errorf("machine is required")
	}
	cfg, err := d.loadConfig(configPath)
	if err != nil {
		return Result{}, err
	}
	host, ok := cfg.HostByName(machine)
	if !ok {
		return Result{}, fmt.Errorf("host %q not found in config", machine)
	}
	result := Result{Machine: host.Name}
	for _, route := range host.SortedRoutes() {
		routeResult := RouteResult{
			Name:   route.Name,
			Target: route.SSHTarget,
		}
		if err := sshsetup.ValidateHostAlias(route.SSHTarget); err != nil {
			return result, fmt.Errorf("route %q target %q cannot be used for SSH config setup; use an SSH config alias as ssh_target: %w", route.Name, route.SSHTarget, err)
		}
		_, err := d.removeForward(sshConfigPath, route.SSHTarget)
		switch {
		case err == nil:
			routeResult.ForwardStatus = "removed"
		case errors.Is(err, sshsetup.ErrNoClipportBlocks):
			routeResult.ForwardStatus = "not present"
		default:
			return result, fmt.Errorf("remove forward for route %q (%s): %w", route.Name, route.SSHTarget, err)
		}
		if err := d.uninstallShims(route.SSHTarget, removeRemoteToken); err != nil {
			return result, fmt.Errorf("uninstall shims for route %q (%s): %w", route.Name, route.SSHTarget, err)
		}
		result.Routes = append(result.Routes, routeResult)
	}
	return result, nil
}
