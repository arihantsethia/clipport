package remote

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
)

type ProbeFunc func(target string) bool
type RouteTester func(route config.Route) (time.Duration, bool)

type Manager struct {
	probe  ProbeFunc
	Tester RouteTester
	Now    func() time.Time
	TTL    time.Duration

	mu   sync.RWMutex
	best map[string]cachedRoute
}

type cachedRoute struct {
	route     config.Route
	checkedAt time.Time
}

const (
	DefaultRouteCacheTTL = time.Hour
	probePayloadSize     = 128 * 1024
	routeTieWindow       = 75 * time.Millisecond
)

func NewManager(probe ProbeFunc) *Manager {
	if probe == nil {
		probe = Probe
	}
	return &Manager{probe: probe, Tester: ProbeUpload, Now: time.Now, TTL: DefaultRouteCacheTTL, best: map[string]cachedRoute{}}
}

func (m *Manager) WarmHost(host config.Host) {
	go m.RefreshHost(host)
}

func (m *Manager) RefreshHost(host config.Host) {
	now := m.now()
	tester := m.Tester
	if tester == nil {
		tester = ProbeUpload
	}
	var best config.Route
	var bestLatency time.Duration
	for _, route := range host.SortedRoutes() {
		latency, ok := tester(route)
		if !ok {
			continue
		}
		if best.Name == "" || latency+routeTieWindow < bestLatency {
			best = route
			bestLatency = latency
		}
	}
	if best.Name != "" {
		m.mu.Lock()
		m.best[host.Name] = cachedRoute{route: best, checkedAt: now}
		m.mu.Unlock()
		return
	}
	for _, route := range host.SortedRoutes() {
		if m.probe(route.SSHTarget) {
			m.mu.Lock()
			m.best[host.Name] = cachedRoute{route: route, checkedAt: now}
			m.mu.Unlock()
			return
		}
	}
	routes := host.SortedRoutes()
	if len(routes) > 0 {
		m.mu.Lock()
		m.best[host.Name] = cachedRoute{route: routes[0], checkedAt: now}
		m.mu.Unlock()
	}
}

func (m *Manager) BestRoute(host config.Host) config.Route {
	m.mu.RLock()
	cached, ok := m.best[host.Name]
	m.mu.RUnlock()
	if ok && m.now().Sub(cached.checkedAt) < m.ttl() {
		return cached.route
	}
	m.RefreshHost(host)
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.best[host.Name].route
}

func (m *Manager) CachedRoute(host config.Host) config.Route {
	m.mu.RLock()
	cached, ok := m.best[host.Name]
	m.mu.RUnlock()
	if ok {
		return cached.route
	}
	routes := host.SortedRoutes()
	if len(routes) == 0 {
		return config.Route{}
	}
	return routes[0]
}

func (m *Manager) InvalidateHost(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.best, name)
}

func (m *Manager) InvalidateAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.best = map[string]cachedRoute{}
}

func (m *Manager) RefreshAll(hosts []config.Host) {
	for _, host := range hosts {
		m.RefreshHost(host)
	}
}

func (m *Manager) RefreshOnNetworkChange(ctx context.Context, hosts []config.Host) {
	go func() {
		changes := NetworkChanges(ctx)
		var timer *time.Timer
		var timerC <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case _, ok := <-changes:
				if !ok {
					return
				}
				if timer == nil {
					timer = time.NewTimer(2 * time.Second)
					timerC = timer.C
					continue
				}
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(2 * time.Second)
			case <-timerC:
				timer = nil
				timerC = nil
				m.InvalidateAll()
				go m.RefreshAll(hosts)
			}
		}
	}()
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) ttl() time.Duration {
	if m.TTL > 0 {
		return m.TTL
	}
	return DefaultRouteCacheTTL
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
	cmd := exec.CommandContext(ctx, "ssh", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=1", target, "true")
	return cmd.Run() == nil
}

func ProbeUpload(route config.Route) (time.Duration, bool) {
	if route.SSHTarget == "" {
		return 0, false
	}
	payload := make([]byte, probePayloadSize)
	remotePath := fmt.Sprintf("/tmp/clipport/route-probe-%d.bin", time.Now().UnixNano())
	cmd := exec.Command("ssh", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", route.SSHTarget, "mkdir -p /tmp/clipport && cat > "+shellQuote(remotePath)+" && rm -f "+shellQuote(remotePath))
	cmd.Stdin = bytes.NewReader(payload)
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return 0, false
	}
	return time.Since(start), true
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
