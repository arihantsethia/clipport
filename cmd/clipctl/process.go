package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func cleanupInstalledAppProcesses(paths []string) error {
	return newProcessCleaner(context.Background()).clean(paths)
}

type processCleaner struct {
	listClipportPIDs func() ([]int, error)
	executablePath   func(int) (string, error)
	signal           func(int, syscall.Signal) error
	isRunning        func(int) bool
	sleep            func(time.Duration)
	waitInterval     time.Duration
	waitTimeout      time.Duration
}

func newProcessCleaner(ctx context.Context) processCleaner {
	return processCleaner{
		listClipportPIDs: func() ([]int, error) { return listClipportPIDs(ctx) },
		executablePath:   func(pid int) (string, error) { return executablePath(ctx, pid) },
		signal:           signalPID,
		isRunning:        processRunning,
		sleep:            time.Sleep,
		waitInterval:     50 * time.Millisecond,
		waitTimeout:      2 * time.Second,
	}
}

func (c processCleaner) clean(paths []string) error {
	wanted := map[string]bool{}
	for _, path := range paths {
		if path != "" {
			wanted[path] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	pids, err := c.listClipportPIDs()
	if err != nil {
		return err
	}
	var matched []int
	for _, pid := range pids {
		executable, err := c.executablePath(pid)
		if err != nil {
			continue
		}
		if !isInstalledAppExecutable(executable, wanted) {
			continue
		}
		if err := c.signal(pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}
		matched = append(matched, pid)
	}
	return c.waitThenKill(matched)
}

func (c processCleaner) waitThenKill(pids []int) error {
	deadline := time.Now().Add(c.waitTimeout)
	for _, pid := range pids {
		for c.isRunning(pid) && time.Now().Before(deadline) {
			c.sleep(c.waitInterval)
		}
		if c.isRunning(pid) {
			if err := c.signal(pid, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return err
			}
		}
	}
	return nil
}

func listClipportPIDs(ctx context.Context) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "pgrep", "-x", "clipport")
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func executablePath(ctx context.Context, pid int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "lsof", "-p", strconv.Itoa(pid), "-a", "-d", "txt", "-Fn")
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path, ok := executablePathFromLsof(string(out))
	if !ok {
		return "", fmt.Errorf("executable path unavailable for pid %d", pid)
	}
	return path, nil
}

func executablePathFromLsof(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "n") && len(line) > 1 {
			return strings.TrimPrefix(line, "n"), true
		}
	}
	return "", false
}

func signalPID(pid int, sig syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(sig)
}

func processRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func isInstalledAppExecutable(executable string, wanted map[string]bool) bool {
	return wanted[executable]
}

func cleanupAppProcesses(ctx context.Context, paths []string) error {
	cleaner := newProcessCleaner(ctx)
	return cleanProcesses(ctx, cleaner, paths)
}

func cleanProcesses(ctx context.Context, cleaner processCleaner, paths []string) error {
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
