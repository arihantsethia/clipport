package menu

import "github.com/arihantsethia/clipport/internal/daemon"

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
	State      State
	Title      string
	Detail     string
	HostLabels []string
}

func (c Checker) DaemonSummary() Summary {
	if c.Status != nil {
		status, err := c.Status()
		if err == nil {
			return Summary{
				State:      StateRunning,
				Title:      "Clipport: Running",
				HostLabels: HostLabels(status),
			}
		}
		return Summary{State: StateStopped, Title: "Clipport: Stopped", Detail: err.Error()}
	}
	return Summary{State: StateStopped, Title: "Clipport: Stopped", Detail: "daemon status unavailable"}
}

func HostLabels(status daemon.Status) []string {
	if len(status.Hosts) == 0 {
		return append([]string(nil), status.ConfigHosts...)
	}
	labels := make([]string, 0, len(status.Hosts))
	for _, host := range status.Hosts {
		labels = append(labels, HostLabel(host))
	}
	return labels
}

func HostLabel(host daemon.HostStatus) string {
	if host.Target == "" {
		return host.Name
	}
	return host.Name + " (" + host.Target + ")"
}
