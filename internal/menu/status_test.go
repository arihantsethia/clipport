package menu

import (
	"errors"
	"strings"
	"testing"

	"github.com/arihantsethia/clipport/internal/daemon"
)

func TestDaemonSummaryUsesSocketStatus(t *testing.T) {
	checker := Checker{
		Status: func() (daemon.Status, error) {
			return daemon.Status{
				ConfigHosts: []string{"devbox"},
				Hosts: []daemon.HostStatus{{
					Name:   "devbox",
					Target: "192.168.1.20",
				}},
			}, nil
		},
	}

	summary := checker.DaemonSummary()

	if summary.State != StateRunning {
		t.Fatalf("state = %q, want running", summary.State)
	}
	if summary.Title != "Clipport: Running" {
		t.Fatalf("title = %q", summary.Title)
	}
	if len(summary.HostLabels) != 1 || summary.HostLabels[0] != "devbox (192.168.1.20)" {
		t.Fatalf("host labels = %+v", summary.HostLabels)
	}
}

func TestDaemonSummaryReportsStoppedWhenSocketStatusFails(t *testing.T) {
	checker := Checker{
		Status: func() (daemon.Status, error) {
			return daemon.Status{}, errors.New("dial unix /tmp/clipport.sock: connect: no such file")
		},
	}

	summary := checker.DaemonSummary()

	if summary.State != StateStopped {
		t.Fatalf("state = %q, want stopped", summary.State)
	}
	if !strings.Contains(summary.Detail, "connect: no such file") {
		t.Fatalf("detail = %q", summary.Detail)
	}
}
