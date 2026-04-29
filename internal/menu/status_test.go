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
				Recent: []daemon.Transfer{{
					Host: "devbox",
					Path: "/tmp/clipport/user/clipboard.png",
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
	if len(summary.RecentTransfers) != 1 {
		t.Fatalf("summary = %+v", summary)
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
