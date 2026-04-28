package sshsetup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/arihantsethia/clipport/internal/registry"
)

const DefaultRemotePort = 18765

var ErrForwardAlreadyInstalled = errors.New("clipport forward already installed")
var ErrNoClipportBlocks = errors.New("no clipport SSH config blocks found")

var hostAliasRE = regexp.MustCompile(`^[A-Za-z0-9._%+-]+$`)

func DefaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

func InstallForward(configPath, host string, remotePort int) (string, error) {
	if err := ValidateHostAlias(host); err != nil {
		return "", err
	}
	if remotePort == 0 {
		remotePort = DefaultRemotePort
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	block := fmt.Sprintf("\n# clipport begin %s\nHost %s\n    RemoteForward 127.0.0.1:%d 127.0.0.1:%d\n# clipport end %s\n", host, host, remotePort, remotePort, host)
	if hasExactLine(data, "# clipport begin "+host) {
		return "", fmt.Errorf("%w for %s", ErrForwardAlreadyInstalled, host)
	}
	backup, err := writeConfigWithBackup(configPath, data, append(data, []byte(block)...))
	if err != nil {
		return "", err
	}
	_ = markForward(host)
	return backup, nil
}

func InstallSessionHook(configPath, host, machine, clipportBin string) (string, error) {
	if err := ValidateHostAlias(host); err != nil {
		return "", err
	}
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return "", fmt.Errorf("machine is required")
	}
	clipportBin = strings.TrimSpace(clipportBin)
	if clipportBin == "" {
		return "", fmt.Errorf("clipport binary path is required")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	marker := "# clipport session begin " + host
	if hasExactLine(data, marker) {
		return "", nil
	}
	block := fmt.Sprintf(
		"\n# clipport session begin %s\nHost %s\n    PermitLocalCommand yes\n    LocalCommand %s session register --machine %s --session-key \"${TERM_SESSION_ID:-}\" --ssh-alias %s --ssh-host %s --ssh-port %s --ssh-user %s\n# clipport session end %s\n",
		host,
		host,
		shellQuote(clipportBin),
		shellQuote(machine),
		shellQuote("%n"),
		shellQuote("%h"),
		shellQuote("%p"),
		shellQuote("%r"),
		host,
	)
	return writeConfigWithBackup(configPath, data, append(data, []byte(block)...))
}

func RemoveForward(configPath, host string) (string, error) {
	if err := ValidateHostAlias(host); err != nil {
		return "", err
	}
	return removeMarkedBlocks(configPath, "# clipport begin "+host, "# clipport end "+host)
}

func RemoveSessionHook(configPath, host string) (string, error) {
	if err := ValidateHostAlias(host); err != nil {
		return "", err
	}
	return removeMarkedBlocks(configPath, "# clipport session begin "+host, "# clipport session end "+host)
}

func RemoveAllClipportBlocks(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	updated, removed, err := removeBlocks(string(data), isClipportBegin, isClipportEnd)
	if err != nil {
		return "", err
	}
	if !removed {
		return "", ErrNoClipportBlocks
	}
	return writeConfigWithBackup(configPath, data, []byte(updated))
}

func ValidateHostAlias(host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if !hostAliasRE.MatchString(host) {
		return fmt.Errorf("invalid SSH host alias %q", host)
	}
	return nil
}

func hasExactLine(data []byte, line string) bool {
	for _, candidate := range strings.Split(string(data), "\n") {
		if strings.TrimRight(candidate, "\r") == line {
			return true
		}
	}
	return false
}

func removeMarkedBlocks(configPath, begin, end string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	updated, removed, err := removeBlocks(string(data), func(line string) bool {
		return strings.TrimRight(line, "\r") == begin
	}, func(line string) bool {
		return strings.TrimRight(line, "\r") == end
	})
	if err != nil {
		return "", err
	}
	if !removed {
		return "", ErrNoClipportBlocks
	}
	return writeConfigWithBackup(configPath, data, []byte(updated))
}

func removeBlocks(text string, isBegin, isEnd func(string) bool) (string, bool, error) {
	lines := strings.SplitAfter(text, "\n")
	var kept []string
	removed := false
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case !inBlock && isBegin(trimmed):
			inBlock = true
			removed = true
			if len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
				kept = kept[:len(kept)-1]
			}
		case inBlock && isEnd(trimmed):
			inBlock = false
		case inBlock:
		default:
			kept = append(kept, line)
		}
	}
	if inBlock {
		return "", false, fmt.Errorf("unterminated clipport SSH config block")
	}
	return strings.Join(kept, ""), removed, nil
}

func isClipportBegin(line string) bool {
	return strings.HasPrefix(line, "# clipport begin ") || strings.HasPrefix(line, "# clipport session begin ")
}

func isClipportEnd(line string) bool {
	return strings.HasPrefix(line, "# clipport end ") || strings.HasPrefix(line, "# clipport session end ")
}

func writeConfigWithBackup(configPath string, current, updated []byte) (string, error) {
	backup := fmt.Sprintf("%s.clipport-%s.bak", configPath, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, current, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(configPath, updated, 0o600); err != nil {
		return "", err
	}
	return backup, nil
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func markForward(host string) error {
	reg, err := registry.Load("")
	if err != nil {
		return err
	}
	reg.UpdateHost(host, func(st registry.HostState) registry.HostState {
		st.ForwardInstalled = true
		return st
	})
	return reg.Save("")
}
