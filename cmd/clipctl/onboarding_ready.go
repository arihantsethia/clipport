package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/token"
)

func finishOnboarding(configPath, socketPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load saved configuration: %w", err)
	}
	if cfg.Local.AppLaunchdPlistPath == "" {
		return fmt.Errorf("app installation is not recorded; install Clipport before running onboarding")
	}
	addr := cfg.Local.HTTPAddr
	if addr == "" {
		addr = "127.0.0.1:18765"
	}
	if !isLoopbackHTTPAddr(addr) {
		return fmt.Errorf("invalid loopback HTTP address %q", addr)
	}
	expectedHosts := make([]string, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		expectedHosts = append(expectedHosts, host.Name)
	}
	// The timeout also bounds launchctl. Each readiness probe gets its own shorter
	// deadline, so a listening but unresponsive daemon cannot hang onboarding.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	run := func(name string, args ...string) error {
		return runOnboardingCommand(ctx, name, args...)
	}
	return startAndWaitForApp(ctx, func() error {
		return stopThenStartApp(ctx, func() error {
			return stopInstalledApp(config.LoadLocalBestEffort, configPath, run, func(paths []string) error {
				return cleanupOnboardingProcesses(ctx, paths)
			}, os.Getuid())
		}, func(ctx context.Context) error {
			return probeDaemonStopped(ctx, socketPath, addr)
		}, func() error {
			return startInstalledAppWithLocal(cfg.Local, run, os.Getuid())
		})
	}, func(ctx context.Context) error {
		return probeAppReady(ctx, socketPath, addr, token.DefaultPath(), expectedHosts)
	})
}

func startAndWaitForApp(ctx context.Context, start func() error, probe func(context.Context) error) error {
	if err := start(); err != nil {
		return fmt.Errorf("start Clipport: %w", err)
	}
	return waitForAppReady(ctx, 100*time.Millisecond, probe)
}

func waitForAppReady(ctx context.Context, interval time.Duration, probe func(context.Context) error) error {
	return waitForAppState(ctx, interval, "readiness", probe)
}

func waitForAppState(ctx context.Context, interval time.Duration, state string, probe func(context.Context) error) error {
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for Clipport %s: %w (last check: %v)", state, err, lastErr)
		}
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		lastErr = probe(attempt)
		cancel()
		if lastErr == nil {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("waiting for Clipport %s: %w (last check: %v)", state, ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

func probeAppReady(ctx context.Context, socketPath, addr, tokenPath string, expectedHosts []string) error {
	if !isLoopbackHTTPAddr(addr) {
		return fmt.Errorf("invalid loopback HTTP address %q", addr)
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("daemon status: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("daemon deadline: %w", err)
		}
	}
	if err := json.NewEncoder(conn).Encode(daemon.Request{Command: "status"}); err != nil {
		return fmt.Errorf("daemon status: %w", err)
	}
	var response daemon.Response
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&response); err != nil {
		return fmt.Errorf("daemon status: %w", err)
	}
	if response.Error != "" {
		return fmt.Errorf("daemon status: %s", response.Error)
	}
	if !sameHosts(response.Status.ConfigHosts, expectedHosts) {
		return fmt.Errorf("daemon has not loaded the selected hosts")
	}
	bearer, err := token.Load(tokenPath)
	if err != nil {
		return fmt.Errorf("HTTP token: %w", err)
	}
	if bearer == "" {
		return fmt.Errorf("HTTP token is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/v1/health", nil)
	if err != nil {
		return fmt.Errorf("HTTP health: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	client := &http.Client{
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP health: %s", resp.Status)
	}
	var health struct {
		OK string `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health); err != nil {
		return fmt.Errorf("HTTP health response: %w", err)
	}
	if health.OK != "true" {
		return fmt.Errorf("HTTP endpoint did not report ready")
	}
	return nil
}

func sameHosts(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, name := range want {
		counts[name]++
	}
	for _, name := range got {
		counts[name]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

// The menu app exits before its child daemon notices the parent has gone.
// Wait for both listeners to disappear before launching a replacement, rather
// than accepting the old daemon's health or racing its HTTP bind.
func stopThenStartApp(ctx context.Context, stop func() error, stopped func(context.Context) error, start func() error) error {
	if err := stop(); err != nil {
		return fmt.Errorf("stop previous app: %w", err)
	}
	if err := waitForAppState(ctx, 100*time.Millisecond, "previous daemon shutdown", stopped); err != nil {
		return err
	}
	return start()
}

func probeDaemonStopped(ctx context.Context, socketPath, addr string) error {
	for _, endpoint := range []struct{ network, address string }{{"unix", socketPath}, {"tcp", addr}} {
		conn, err := (&net.Dialer{}).DialContext(ctx, endpoint.network, endpoint.address)
		if err == nil {
			conn.Close()
			return fmt.Errorf("previous daemon still listening on %s", endpoint.network)
		}
		if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
			return fmt.Errorf("check previous %s listener: %w", endpoint.network, err)
		}
	}
	return nil
}

func runOnboardingCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.CombinedOutput()
	if err != nil {
		if detail := strings.TrimSpace(string(out)); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

func cleanupOnboardingProcesses(ctx context.Context, paths []string) error {
	cleaner := newAppProcessCleaner()
	cleaner.listClipportPIDs = func() ([]int, error) { return listClipportPIDsContext(ctx) }
	cleaner.executablePath = func(pid int) (string, error) { return executablePathForPIDContext(ctx, pid) }
	return cleanAppProcessesContext(ctx, cleaner, paths)
}

func cleanAppProcessesContext(ctx context.Context, cleaner appProcessCleaner, paths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	running, signal := cleaner.isRunning, cleaner.signal
	cleaner.isRunning = func(pid int) bool { return ctx.Err() == nil && running(pid) }
	cleaner.signal = func(pid int, sig syscall.Signal) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return signal(pid, sig)
	}
	cleaner.sleep = func(delay time.Duration) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	err := cleaner.clean(paths)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
