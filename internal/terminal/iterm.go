package terminal

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

var userHostRE = regexp.MustCompile(`([A-Za-z0-9._%+-]+)@([A-Za-z0-9._-]+)`)
var localShellTitleRE = regexp.MustCompile(`^-?(zsh|bash|fish|sh|tmux)$`)

type ItermProvider struct {
	Runner func(name string, args ...string) ([]byte, error)
}

func (p ItermProvider) ActiveSession() (Session, error) {
	runner := p.Runner
	if runner == nil {
		runner = func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).Output()
		}
	}
	out, err := runner("osascript", "-e", `tell application "iTerm2" to if it is running then tell current session of current window to {name, id}`)
	if err != nil {
		return Session{}, err
	}
	title, key := parseSessionInfo(string(bytes.Trim(out, "\x00\r\n\t ")))
	if title == "" {
		return Session{}, errors.New("active iTerm session has no title")
	}
	host := ExtractHost(title)
	kind := SessionRemote
	if host == "" {
		kind = classifyLocalTitle(title)
	}
	return Session{
		Terminal:     "iterm",
		SessionKey:   key,
		DetectedHost: host,
		RawTitle:     title,
		Kind:         kind,
	}, nil
}

func parseSessionInfo(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	title := raw
	key := raw
	if idx := strings.LastIndex(raw, ", "); idx >= 0 && strings.TrimSpace(raw[idx+2:]) != "" {
		title = strings.TrimSpace(raw[:idx])
		key = strings.TrimSpace(raw[idx+2:])
	}
	return title, key
}

func ExtractHost(title string) string {
	m := userHostRE.FindStringSubmatch(title)
	if len(m) == 3 {
		return m[2]
	}
	return ""
}

func classifyLocalTitle(title string) SessionKind {
	normalized := strings.TrimSpace(strings.ToLower(title))
	if localShellTitleRE.MatchString(normalized) {
		return SessionLocal
	}
	return SessionUnknown
}
