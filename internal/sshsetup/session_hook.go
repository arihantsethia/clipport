package sshsetup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Replace only this alias's managed block, at its original position. OpenSSH
// uses the first obtained option, so moving a hook can override user settings.
func reconcileSessionBlock(text, host, block string) (string, error) {
	begin, end := "# clipport session begin "+host, "# clipport session end "+host
	var out strings.Builder
	inside, found := false, false
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case trimmed == begin:
			if inside || found {
				return "", fmt.Errorf("duplicate clipport SSH session block for %s", host)
			}
			inside, found = true, true
			out.WriteString(strings.TrimPrefix(block, "\n"))
		case trimmed == end:
			if !inside {
				return "", fmt.Errorf("unmatched clipport SSH session end for %s", host)
			}
			inside = false
		case inside:
			if isClipportBegin(trimmed) || isClipportEnd(trimmed) {
				return "", fmt.Errorf("nested clipport SSH session block for %s", host)
			}
		default:
			out.WriteString(line)
		}
	}
	if inside {
		return "", fmt.Errorf("unterminated clipport SSH session block for %s", host)
	}
	if !found {
		out.WriteString(block)
	}
	return out.String(), nil
}

// Ask OpenSSH to evaluate the prospective config, including Host wildcards and
// Include files. -G does not connect or run LocalCommand, but OpenSSH can run
// existing Match exec predicates while evaluating configuration. This catches
// user settings that would otherwise silently shadow the hook.
func validateSessionHook(configPath, host, updated, block string) error {
	candidate, err := os.CreateTemp(filepath.Dir(configPath), ".clipport-ssh-check-*")
	if err != nil {
		return err
	}
	defer os.Remove(candidate.Name())
	if _, err := candidate.WriteString(updated); err != nil {
		candidate.Close()
		return err
	}
	if err := candidate.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ssh", "-G", "-F", candidate.Name(), "--", host).Output()
	if err != nil {
		return fmt.Errorf("validate SSH session hook for %s: %w", host, err)
	}
	want := ""
	for _, line := range strings.Split(block, "\n") {
		if command, ok := strings.CutPrefix(strings.TrimSpace(line), "LocalCommand "); ok {
			want = command
		}
	}
	command, permitted := "", false
	for _, line := range strings.Split(string(output), "\n") {
		if value, ok := strings.CutPrefix(line, "localcommand "); ok {
			command = value
		}
		if line == "permitlocalcommand yes" {
			permitted = true
		}
	}
	if !permitted || command != want {
		return fmt.Errorf("SSH settings for %s override the Clipport session hook; review LocalCommand and PermitLocalCommand in %s and its Include files (no changes saved)", host, configPath)
	}
	return nil
}
