package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
)

func TestStartAndWaitForAppStopsOnStartupFailure(t *testing.T) {
	called := false
	err := startAndWaitForApp(context.Background(), func() error { return errors.New("launchctl failed") }, func(context.Context) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "start Clipport: launchctl failed") || called {
		t.Fatalf("err=%v, probe called=%v", err, called)
	}
}

func TestWaitForAppReadyRetriesDelayedDaemon(t *testing.T) {
	attempts := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := waitForAppReady(ctx, time.Millisecond, func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("probe has no deadline")
		}
		attempts++
		if attempts < 3 {
			return errors.New("starting")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestWaitForAppReadyReportsLastFailureAtDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := waitForAppReady(ctx, time.Millisecond, func(context.Context) error { return errors.New("HTTP health: 401 Unauthorized") })
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("err=%v", err)
	}
}

func readinessSocket(t *testing.T, hosts []string) string {
	t.Helper()
	// Keep the Unix socket path short on macOS (104-byte sockaddr_un limit).
	dir, err := os.MkdirTemp("", "clipport-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { listener.Close(); <-done })
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var request daemon.Request
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			if json.NewDecoder(conn).Decode(&request) == nil && request.Command == "status" {
				_ = json.NewEncoder(conn).Encode(daemon.Response{Status: daemon.Status{ConfigHosts: hosts}})
			}
			conn.Close()
		}
	}()
	return path
}

func TestProbeAppReadyUsesExplicitEndpointsAndToken(t *testing.T) {
	socket := readinessSocket(t, []string{"dev"})
	for _, test := range []struct {
		name, token, body string
		status            int
		want              string
	}{
		{"ready", "secret", `{"ok":"true"}`, 200, ""},
		{"bad token", "wrong", "", 401, "401"},
		{"not ready", "secret", `{"ok":"false"}`, 200, "did not report ready"},
		{"bad health", "secret", "broken", 200, "HTTP health response"},
		{"redirect", "secret", "", 302, "302"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tokenPath := filepath.Join(t.TempDir(), "custom-token")
			if err := os.WriteFile(tokenPath, []byte(test.token), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/health" {
					t.Errorf("path=%s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+test.token {
					t.Error("missing auth header")
				}
				if test.status == 302 {
					w.Header().Set("Location", "http://example.invalid/leak")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := probeAppReady(ctx, socket, strings.TrimPrefix(server.URL, "http://"), tokenPath, []string{"dev"})
			if test.want == "" && err != nil {
				t.Fatal(err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("err=%v want=%s", err, test.want)
			}
		})
	}
}

func TestProbeAppReadyRejectsStaleConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := probeAppReady(ctx, readinessSocket(t, []string{"old"}), "127.0.0.1:1", "unused", []string{"new"})
	if err == nil || !strings.Contains(err.Error(), "selected hosts") {
		t.Fatalf("err=%v", err)
	}
}

func TestProbeAppReadyRejectsNonLoopbackBeforeConnecting(t *testing.T) {
	err := probeAppReady(context.Background(), "unused", "example.com:80", "unused", nil)
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err=%v", err)
	}
}

func TestProbeAppReadyBoundsUnresponsiveDaemon(t *testing.T) {
	dir, err := os.MkdirTemp("", "clipport-stall-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = probeAppReady(ctx, listener.Addr().String(), "127.0.0.1:1", "unused", nil)
	if err == nil || !strings.Contains(err.Error(), "daemon status") {
		t.Fatalf("err=%v", err)
	}
}

func TestStopThenStartAppWaitsForOldDaemon(t *testing.T) {
	checks := 0
	stopped, started := false, false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := stopThenStartApp(ctx, func() error { stopped = true; return nil }, func(context.Context) error {
		if !stopped || started {
			t.Fatal("shutdown checked out of order")
		}
		checks++
		if checks < 3 {
			return errors.New("old daemon still alive")
		}
		return nil
	}, func() error { started = true; return nil })
	if err != nil || !started || checks != 3 {
		t.Fatalf("err=%v started=%v checks=%d", err, started, checks)
	}
}

func TestStopThenStartAppDoesNotStartOnShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := false
	err := stopThenStartApp(ctx, func() error { return nil }, func(context.Context) error { return errors.New("old HTTP listener remains") }, func() error { started = true; return nil })
	if started || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "previous daemon shutdown") {
		t.Fatalf("started=%v err=%v", started, err)
	}
}

func TestProbeDaemonStoppedWaitsForBothListeners(t *testing.T) {
	dir, err := os.MkdirTemp("", "clipport-stop-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")
	unix, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	addr := tcp.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probeDaemonStopped(ctx, socketPath, addr); err == nil {
		t.Fatal("active Unix listener treated as stopped")
	}
	unix.Close()
	if err := probeDaemonStopped(ctx, socketPath, addr); err == nil {
		t.Fatal("active HTTP listener treated as stopped")
	}
	tcp.Close()
	if err := probeDaemonStopped(ctx, socketPath, addr); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingStopAcceptsFreshInstallLaunchctlError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cleaned := false
	err := stopInstalledApp(func(string) (config.LocalConfig, error) {
		return config.LocalConfig{AppLaunchdPlistPath: "/tmp/clipport-test.plist", AppPath: "/tmp/Clipport.app", BinDir: "/tmp/bin"}, nil
	}, "unused", func(name string, args ...string) error {
		if name != "launchctl" || args[0] != "bootout" {
			t.Fatalf("unexpected command %s %v", name, args)
		}
		return runOnboardingCommand(ctx, "sh", "-c", "printf 'Boot-out failed: 5: Input/output error\\n' >&2; exit 5")
	}, func([]string) error { cleaned = true; return nil }, 501)
	if err != nil || !cleaned {
		t.Fatalf("err=%v cleanup=%v", err, cleaned)
	}
}

func TestOnboardingCommandPreservesRealStartupFailure(t *testing.T) {
	err := runOnboardingCommand(context.Background(), "sh", "-c", "printf 'operation not permitted' >&2; exit 5")
	if err == nil || isLaunchctlNotLoaded(err) || !strings.Contains(err.Error(), "operation not permitted") {
		t.Fatalf("err=%v", err)
	}
}

func TestOnboardingCleanupGracePeriodHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := 0
	cleaner := appProcessCleaner{
		listClipportPIDs: func() ([]int, error) { return []int{123}, nil },
		executablePath:   func(int) (string, error) { return "/expected/clipport", nil },
		signal: func(_ int, sig syscall.Signal) error {
			signals++
			if sig != syscall.SIGTERM {
				t.Fatal("sent kill after cancellation")
			}
			return nil
		},
		isRunning:    func(int) bool { cancel(); return true },
		waitInterval: time.Second,
		waitTimeout:  time.Second,
	}
	err := cleanAppProcessesContext(ctx, cleaner, []string{"/expected/clipport"})
	if !errors.Is(err, context.Canceled) || signals != 1 {
		t.Fatalf("err=%v signals=%d", err, signals)
	}
}

func TestOnboardingCleanupCommandsHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listClipportPIDsContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pgrep err=%v", err)
	}
	if _, err := executablePathForPIDContext(ctx, os.Getpid()); !errors.Is(err, context.Canceled) {
		t.Fatalf("lsof err=%v", err)
	}
}
