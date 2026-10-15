package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestIsInstalledAppCommandRequiresExactExecutablePath(t *testing.T) {
	wanted := map[string]bool{
		"/tmp/Clipport.app/Contents/MacOS/clipport":             true,
		"/tmp/Clipport With Spaces.app/Contents/MacOS/clipport": true,
		"/tmp/bin/clipport": true,
	}
	for _, executable := range []string{
		"/tmp/Clipport.app/Contents/MacOS/clipport",
		"/tmp/Clipport With Spaces.app/Contents/MacOS/clipport",
		"/tmp/bin/clipport",
	} {
		if !isInstalledAppExecutable(executable, wanted) {
			t.Fatalf("executable %q did not match", executable)
		}
	}
	for _, executable := range []string{
		"/tmp/bin/clipportd",
		"/tmp/other/clipport paste",
		"",
	} {
		if isInstalledAppExecutable(executable, wanted) {
			t.Fatalf("executable %q should not match", executable)
		}
	}
}

func TestExecutablePathFromLsofHandlesSpaces(t *testing.T) {
	out := "p123\nftxt\nn/tmp/Clipport With Spaces.app/Contents/MacOS/clipport\n"
	got, ok := executablePathFromLsof(out)
	if !ok {
		t.Fatal("expected executable path")
	}
	want := "/tmp/Clipport With Spaces.app/Contents/MacOS/clipport"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestCleanupInstalledAppProcessesWaitsThenKillsStillRunningProcess(t *testing.T) {
	var signals []syscall.Signal
	cleanup := processCleaner{
		listClipportPIDs: func() ([]int, error) { return []int{42}, nil },
		executablePath:   func(int) (string, error) { return "/tmp/bin/clipport", nil },
		signal: func(pid int, sig syscall.Signal) error {
			if pid != 42 {
				t.Fatalf("pid = %d", pid)
			}
			signals = append(signals, sig)
			return nil
		},
		isRunning:    func(int) bool { return true },
		sleep:        func(time.Duration) {},
		waitInterval: time.Millisecond,
		waitTimeout:  0,
	}

	if err := cleanup.clean([]string{"/tmp/bin/clipport"}); err != nil {
		t.Fatal(err)
	}
	want := []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}
	if len(signals) != len(want) {
		t.Fatalf("signals = %v, want %v", signals, want)
	}
	for i := range want {
		if signals[i] != want[i] {
			t.Fatalf("signals = %v, want %v", signals, want)
		}
	}
}

func TestOnboardingCleanupGracePeriodHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := 0
	cleaner := processCleaner{
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
	err := cleanProcesses(ctx, cleaner, []string{"/expected/clipport"})
	if !errors.Is(err, context.Canceled) || signals != 1 {
		t.Fatalf("err=%v signals=%d", err, signals)
	}
}

func TestOnboardingCleanupCommandsHonorCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listClipportPIDs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pgrep err=%v", err)
	}
	if _, err := executablePath(ctx, os.Getpid()); !errors.Is(err, context.Canceled) {
		t.Fatalf("lsof err=%v", err)
	}
}
