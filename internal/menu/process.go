package menu

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type CommandFactory func(name string, args ...string) *exec.Cmd

type DaemonProcess struct {
	BinPath    string
	ConfigPath string
	HTTPAddr   string
	OutLogPath string
	ErrLogPath string
	Command    CommandFactory

	mu  sync.Mutex
	cmd *exec.Cmd
}

func (p *DaemonProcess) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil && p.cmd.ProcessState == nil {
		return nil
	}
	if p.BinPath == "" {
		return errors.New("clipportd path is empty")
	}
	args := []string{"--config", p.ConfigPath}
	if p.HTTPAddr != "" {
		args = append(args, "--http", p.HTTPAddr)
	}
	args = append(args, "--parent-pid", fmt.Sprint(os.Getpid()))
	factory := p.Command
	if factory == nil {
		factory = exec.Command
	}
	cmd := factory(p.BinPath, args...)
	stdout, stderr, err := p.openLogs()
	if err != nil {
		return err
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	p.cmd = cmd
	go func() {
		_ = cmd.Wait()
		_ = stdout.Close()
		_ = stderr.Close()
		p.mu.Lock()
		if p.cmd == cmd {
			p.cmd = nil
		}
		p.mu.Unlock()
	}()
	return nil
}

func (p *DaemonProcess) Stop() error {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return nil
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	deadline := time.After(2 * time.Second)
	for {
		p.mu.Lock()
		running := p.cmd == cmd
		p.mu.Unlock()
		if !running {
			return nil
		}
		select {
		case <-deadline:
			return cmd.Process.Kill()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (p *DaemonProcess) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	return p.Start()
}

func (p *DaemonProcess) openLogs() (io.WriteCloser, io.WriteCloser, error) {
	stdout, err := openLog(p.OutLogPath)
	if err != nil {
		return nil, nil, err
	}
	stderr, err := openLog(p.ErrLogPath)
	if err != nil {
		_ = stdout.Close()
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func openLog(path string) (io.WriteCloser, error) {
	if path == "" {
		return nopWriteCloser{io.Discard}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	return file, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}
