package terminal

import (
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

var userHostRE = regexp.MustCompile(`([A-Za-z0-9._%+-]+)@([A-Za-z0-9._-]+)`)

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
	out, err := runner("osascript", "-e", `tell application "iTerm2" to if it is running then tell current session of current window to get name`)
	if err != nil {
		return Session{}, err
	}
	title := strings.TrimSpace(string(bytes.Trim(out, "\x00\r\n\t ")))
	if title == "" {
		return Session{}, errors.New("active iTerm session has no title")
	}
	host := ExtractHost(title)
	return Session{
		Terminal:     "iterm",
		SessionKey:   title,
		DetectedHost: host,
		RawTitle:     title,
	}, nil
}

func ExtractHost(title string) string {
	m := userHostRE.FindStringSubmatch(title)
	if len(m) == 3 {
		return m[2]
	}
	return ""
}
