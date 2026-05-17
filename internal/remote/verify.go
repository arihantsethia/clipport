package remote

import (
	"fmt"
	"os/exec"
	"strings"
)

func VerifyFile(target, remotePath string) error {
	cmd := exec.Command("ssh", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", target, "test -s "+shellQuote(remotePath)+" && file -b "+shellQuote(remotePath))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify %s failed: %w: %s", remotePath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func CheckWritableDir(target string) error {
	cmd := exec.Command("ssh", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", target, "mkdir -p /tmp/clipport && test -w /tmp/clipport")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote /tmp/clipport not writable on %s: %w: %s", target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func CheckForwardHealth(target, addr string) error {
	cmd := forwardHealthCommand(target, addr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote forward unavailable on %s: %w: %s", target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func forwardHealthCommand(target, addr string) *exec.Cmd {
	script := fmt.Sprintf(
		`token=$(cat "$HOME/.config/clipport/token" 2>/dev/null || true); test -n "$token"; curl -fsS -H "Authorization: Bearer $token" %s`,
		shellQuote(fmt.Sprintf("http://%s/v1/health", addr)),
	)
	return exec.Command("ssh", "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", target, script)
}
