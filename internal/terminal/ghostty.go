package terminal

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Ghostty restores AppleScript UUIDs with windows, so include the running app's
// launch identity rather than allowing a saved binding to survive an app restart.
const ghosttyInstanceScript = `ObjC.import('AppKit');
function run() {
  const apps = $.NSRunningApplication.runningApplicationsWithBundleIdentifier('com.mitchellh.ghostty');
  if (apps.count !== 1) throw new Error('Ghostty is not running as a single app instance');
  const app = apps.objectAtIndex(0);
  return JSON.stringify({pid: app.processIdentifier, started: app.launchDate.timeIntervalSince1970});
}`

const ghosttyActiveScript = `tell application "Ghostty"
  if not running then error "Ghostty is not running"
  if not frontmost then error "Focus the Ghostty pane to use with Clipport"
  return id of focused terminal of selected tab of front window
end tell`

const ghosttyPasteScript = `on run argv
  tell application "Ghostty"
    if not running then error "Ghostty is not running"
    set targetTerminal to terminal id (item 1 of argv)
  end tell
  set payload to read (POSIX file (item 2 of argv)) as «class utf8»
  tell application "Ghostty" to input text payload to targetTerminal
end run`

var ghosttyIDRE = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

type GhosttyProvider struct {
	Runner func(name string, args ...string) ([]byte, error)
}

func (p GhosttyProvider) ActiveSession() (Session, error) {
	generation, err := p.instance()
	if err != nil {
		return Session{}, err
	}
	out, err := p.run("-e", ghosttyActiveScript)
	if err != nil {
		return Session{}, fmt.Errorf("capture Ghostty pane (requires Ghostty 1.3+ and Automation permission): %w", err)
	}
	id := strings.TrimSpace(string(out))
	if !ghosttyIDRE.MatchString(id) {
		return Session{}, errors.New("Ghostty returned an invalid AppleScript terminal ID")
	}
	if current, err := p.instance(); err != nil {
		return Session{}, err
	} else if current != generation {
		return Session{}, errors.New("Ghostty restarted while capturing the pane")
	}
	return Session{Terminal: "ghostty", SessionKey: "ghostty:" + generation + ":" + id}, nil
}

// PasteText addresses the captured UUID, never the newly focused pane. Clipboard
// contents travel in a private temporary file, not process arguments or code.
func (p GhosttyProvider) PasteText(session Session, text string) error {
	parts := strings.Split(session.SessionKey, ":")
	if len(parts) != 3 || parts[0] != "ghostty" || !ghosttyIDRE.MatchString(parts[2]) {
		return errors.New("invalid Ghostty session key")
	}
	generation, err := p.instance()
	if err != nil {
		return err
	}
	if generation != parts[1] {
		return errors.New("Ghostty restarted; register the new session before pasting")
	}
	file, err := os.CreateTemp("", "clipport-paste-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	_, err = p.run("-e", ghosttyPasteScript, parts[2], file.Name())
	return err
}

func (p GhosttyProvider) instance() (string, error) {
	out, err := p.run("-l", "JavaScript", "-e", ghosttyInstanceScript)
	if err != nil {
		return "", err
	}
	var instance struct {
		PID     int     `json:"pid"`
		Started float64 `json:"started"`
	}
	if err := json.Unmarshal(out, &instance); err != nil {
		return "", fmt.Errorf("read Ghostty launch identity: %w", err)
	}
	if instance.PID <= 0 || instance.Started <= 0 {
		return "", errors.New("Ghostty launch identity is unavailable")
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%.9f", instance.PID, instance.Started)))
	return fmt.Sprintf("%x", key), nil
}

func (p GhosttyProvider) run(args ...string) ([]byte, error) {
	if p.Runner != nil {
		return p.Runner("osascript", args...)
	}
	out, err := exec.Command("osascript", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("Ghostty automation: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
