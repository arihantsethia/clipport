package doctor

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/registry"
	"github.com/arihantsethia/clipport/internal/remote"
	"github.com/arihantsethia/clipport/internal/sshsetup"
	"github.com/arihantsethia/clipport/internal/token"
	"github.com/arihantsethia/clipport/internal/uninstall"
)

type Check struct {
	Name   string
	OK     bool
	Detail string
}

func Run(configPath, socketPath string) []Check {
	checks := []Check{
		checkPngpaste(),
		checkDaemon(socketPath),
		checkHTTP(),
		checkLaunchAgent(),
		checkItermBinding(),
		checkRegistry(),
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		checks = append(checks, Check{Name: "config", OK: false, Detail: err.Error()})
		return checks
	}
	checks = append(checks, Check{Name: "config", OK: true, Detail: configPath})
	forwardAddr := installedHTTPAddr()
	for _, host := range cfg.Hosts {
		for _, route := range host.SortedRoutes() {
			name := fmt.Sprintf("ssh %s/%s", host.Name, route.Name)
			checks = append(checks, Check{Name: name, OK: remote.Probe(route.SSHTarget), Detail: route.SSHTarget})
			checks = append(checks, checkRemoteTmp(host.Name, route))
			checks = append(checks, checkRemoteForward(host.Name, route, forwardAddr))
		}
	}
	return checks
}

var checkForwardHealth = remote.CheckForwardHealth

func checkHTTP() Check {
	bearer, err := token.Load("")
	if err != nil {
		return Check{Name: "http api", OK: false, Detail: err.Error()}
	}
	addr := installedHTTPAddr()
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/v1/health", addr), nil)
	if err != nil {
		return Check{Name: "http api", OK: false, Detail: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Check{Name: "http api", OK: false, Detail: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Check{Name: "http api", OK: false, Detail: resp.Status}
	}
	return Check{Name: "http api", OK: true, Detail: addr}
}

func installedHTTPAddr() string {
	if addr := os.Getenv("CLIPPORT_HTTP"); addr != "" {
		return addr
	}
	if local, err := config.LoadLocalBestEffort(""); err == nil && local.HTTPAddr != "" {
		return local.HTTPAddr
	}
	return fmt.Sprintf("127.0.0.1:%d", sshsetup.DefaultRemotePort)
}

func Print(checks []Check) {
	for _, c := range checks {
		mark := "ok"
		if !c.OK {
			mark = "fail"
		}
		if c.Detail == "" {
			fmt.Printf("%-4s %s\n", mark, c.Name)
		} else {
			fmt.Printf("%-4s %-18s %s\n", mark, c.Name, c.Detail)
		}
	}
}

func checkPngpaste() Check {
	for _, path := range []string{"pngpaste", "/opt/homebrew/bin/pngpaste", "/usr/local/bin/pngpaste"} {
		resolved, err := exec.LookPath(path)
		if err == nil {
			return Check{Name: "pngpaste", OK: true, Detail: resolved}
		}
	}
	return Check{Name: "pngpaste", OK: false, Detail: "install with: brew install pngpaste"}
}

func checkDaemon(socketPath string) Check {
	resp, err := daemon.Send(socketPath, daemon.Request{Command: "status"})
	if err != nil {
		return Check{Name: "daemon", OK: false, Detail: err.Error()}
	}
	detail := fmt.Sprintf("%d hosts, %d recent transfers", len(resp.Status.ConfigHosts), len(resp.Status.Recent))
	return Check{Name: "daemon", OK: true, Detail: detail}
}

func checkLaunchAgent() Check {
	cmd := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), uninstall.AppLaunchdLabel))
	if err := cmd.Run(); err != nil {
		return Check{Name: "launchd", OK: false, Detail: uninstall.AppLaunchdLabel + " not running"}
	}
	return Check{Name: "launchd", OK: true, Detail: uninstall.AppLaunchdLabel}
}

func checkItermBinding() Check {
	home, err := os.UserHomeDir()
	if err != nil {
		return Check{Name: "iterm key", OK: false, Detail: err.Error()}
	}
	path := filepath.Join(home, "Library", "Preferences", "com.googlecode.iterm2.plist")
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{Name: "iterm key", OK: false, Detail: err.Error()}
	}
	if strings.Contains(string(data), "clipctl paste") {
		return Check{Name: "iterm key", OK: true, Detail: "clipctl paste"}
	}
	return Check{Name: "iterm key", OK: false, Detail: "binding not found"}
}

func checkRegistry() Check {
	reg, err := registry.Load("")
	if err != nil {
		return Check{Name: "registry", OK: false, Detail: err.Error()}
	}
	return Check{Name: "registry", OK: true, Detail: fmt.Sprintf("%d hosts", len(reg.Hosts))}
}

func checkRemoteTmp(hostName string, route config.Route) Check {
	name := fmt.Sprintf("tmp %s/%s", hostName, route.Name)
	err := remote.CheckWritableDir(route.SSHTarget)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	return Check{Name: name, OK: true, Detail: "/tmp/clipport writable"}
}

func checkRemoteForward(hostName string, route config.Route, addr string) Check {
	name := fmt.Sprintf("forward %s/%s", hostName, route.Name)
	if err := checkForwardHealth(route.SSHTarget, addr); err != nil {
		return Check{Name: name, OK: false, Detail: fmt.Sprintf("%v; reconnect SSH for this host", err)}
	}
	return Check{Name: name, OK: true, Detail: fmt.Sprintf("remote can reach %s", addr)}
}

func DefaultConfigPath() string {
	return config.DefaultPath()
}
