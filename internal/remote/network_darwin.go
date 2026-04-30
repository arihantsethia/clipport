//go:build darwin

package remote

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"time"
)

func NetworkChanges(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		for {
			if ctx.Err() != nil {
				return
			}
			if err := watchRouteMonitor(ctx, ch); err != nil && ctx.Err() == nil {
				time.Sleep(30 * time.Second)
			}
		}
	}()
	return ch
}

func watchRouteMonitor(ctx context.Context, ch chan<- struct{}) error {
	cmd := exec.CommandContext(ctx, "route", "-n", "monitor")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		select {
		case ch <- struct{}{}:
		case <-ctx.Done():
			_ = cmd.Wait()
			return ctx.Err()
		}
	}
	waitErr := cmd.Wait()
	if err := scanner.Err(); err != nil {
		return err
	}
	return waitErr
}
