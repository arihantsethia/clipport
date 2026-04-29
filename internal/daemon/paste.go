package daemon

import (
	"fmt"
	"os/exec"
	"strings"
)

type AppleScriptPaster struct{}

func (AppleScriptPaster) Paste() error {
	cmd := exec.Command("osascript", "-e", `tell application "System Events" to keystroke "v" using command down`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Cmd+V AppleScript failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
