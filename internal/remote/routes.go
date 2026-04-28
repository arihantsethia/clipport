package remote

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
)

type ProbeFunc func(target string) bool

type Manager struct {
	probe ProbeFunc
	mu    sync.RWMutex
	best  map[string]config.Route
}

func NewManager(probe ProbeFunc) *Manager {
	if probe == nil {
		probe = Probe
	}
	return &Manager{probe: probe, best: map[string]config.Route{}}
}

func (m *Manager) WarmHost(host config.Host) {
	go m.RefreshHost(host)
}

func (m *Manager) RefreshHost(host config.Host) {
	for _, route := range host.SortedRoutes() {
		if m.probe(route.SSHTarget) {
			m.mu.Lock()
			m.best[host.Name] = route
			m.mu.Unlock()
			return
		}
	}
	routes := host.SortedRoutes()
	if len(routes) > 0 {
		m.mu.Lock()
		m.best[host.Name] = routes[0]
		m.mu.Unlock()
	}
}

func (m *Manager) BestRoute(host config.Host) config.Route {
	m.mu.RLock()
	route, ok := m.best[host.Name]
	m.mu.RUnlock()
	if ok {
		return route
	}
	m.RefreshHost(host)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.best[host.Name]
}

func Probe(target string) bool {
	host := sshTargetHost(target)
	if host != "" {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "22"), 250*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=1", target, "true")
	return cmd.Run() == nil
}

func sshTargetHost(target string) string {
	if strings.Contains(target, " ") {
		return ""
	}
	if strings.Contains(target, "@") {
		parts := strings.Split(target, "@")
		return parts[len(parts)-1]
	}
	if strings.Contains(target, ".") || net.ParseIP(target) != nil {
		return target
	}
	return ""
}
