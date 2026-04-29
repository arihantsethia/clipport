package menu

import (
	"strings"

	"github.com/arihantsethia/clipport/internal/daemon"
)

type State string

const (
	StateRunning State = "running"
	StateStopped State = "stopped"
)

type StatusFunc func() (daemon.Status, error)

type Checker struct {
	Status StatusFunc
}

type Summary struct {
	State           State
	Title           string
	Detail          string
	ConfigHosts     []string
	RecentTransfers []daemon.Transfer
}

func (c Checker) DaemonSummary() Summary {
	if c.Status != nil {
		status, err := c.Status()
		if err == nil {
			return Summary{
				State:           StateRunning,
				Title:           "Clipport: Running",
				ConfigHosts:     append([]string(nil), status.ConfigHosts...),
				RecentTransfers: append([]daemon.Transfer(nil), status.Recent...),
			}
		}
		return Summary{State: StateStopped, Title: "Clipport: Stopped", Detail: err.Error()}
	}
	return Summary{State: StateStopped, Title: "Clipport: Stopped", Detail: "daemon status unavailable"}
}

func TransferLabel(t daemon.Transfer) string {
	parts := []string{}
	if t.Host != "" {
		parts = append(parts, t.Host)
	}
	if t.Path != "" {
		parts = append(parts, t.Path)
	}
	if len(parts) == 0 {
		return "transfer"
	}
	return strings.Join(parts, "  ")
}
