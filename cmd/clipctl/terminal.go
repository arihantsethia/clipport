package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/daemon"
	"github.com/arihantsethia/clipport/internal/terminal"
)

func resolveSessionKey(name, paneID, explicit string, getenv func(string) string, ghostty func() (terminal.Session, error)) (string, error) {
	if explicit != "" {
		if name != "" || paneID != "" {
			return "", errors.New("--session-key cannot be combined with --terminal or --pane-id")
		}
		return explicit, nil
	}
	if paneID != "" && name != "wezterm" {
		return "", errors.New("--pane-id requires --terminal wezterm")
	}
	switch name {
	case "":
		return terminal.SessionKeyFromEnv(getenv)
	case "ghostty":
		session, err := ghostty()
		return session.SessionKey, err
	case "wezterm":
		if paneID == "" {
			paneID = getenv("WEZTERM_PANE")
		}
		return terminal.WeztermSessionKey(getenv("WEZTERM_UNIX_SOCKET"), paneID)
	default:
		return "", fmt.Errorf("unsupported terminal %q; use ghostty or wezterm", name)
	}
}

func runPaste(socketPath string, debug bool, args []string) error {
	flags := flag.NewFlagSet("paste", flag.ContinueOnError)
	name := flags.String("terminal", "", "terminal: ghostty or wezterm")
	paneID := flags.String("pane-id", "", "WezTerm pane ID")
	explicit := flags.String("session-key", "", "explicit terminal session key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("paste does not accept positional arguments")
	}
	key, err := resolveSessionKey(*name, *paneID, *explicit, os.Getenv, terminal.GhosttyProvider{}.ActiveSession)
	if err != nil {
		return err
	}
	output, err := fetchPaste(socketPath, key, debug)
	if err == nil {
		fmt.Print(output)
	}
	return err
}

func fetchPaste(socketPath, key string, debug bool) (string, error) {
	command := "paste"
	if terminal.RequiresExplicitBinding(key) {
		command = "paste_bound"
	}
	resp, err := daemon.Send(socketPath, daemon.Request{Command: command, SessionKey: key})
	// New terminal integrations require explicit bindings. In particular, never
	// ask iTerm's focused session to repair an unrelated Ghostty or WezTerm pane.
	if err != nil && !terminal.RequiresExplicitBinding(key) && isUnmappedSessionFailure(resp) {
		if repaired, _ := repairUnmappedSession(socketPath, key, config.LoadDefault, terminal.ItermProvider{}.ActiveSession, confirmRepairMachine); repaired {
			resp, err = daemon.Send(socketPath, daemon.Request{Command: "paste", SessionKey: key})
		}
	}
	if err != nil {
		if command == "paste_bound" && resp.Error == "unknown command" {
			return "", errors.New("restart Clipport with the updated daemon before using this terminal integration")
		}
		return "", errors.New(pasteErrorMessage(resp, err, debug))
	}
	return pasteOutput(resp), nil
}

func runPasteTo(socketPath string, debug bool, args []string) error {
	flags := flag.NewFlagSet("paste-to", flag.ContinueOnError)
	name := flags.String("terminal", "", "terminal: ghostty")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *name != "ghostty" || flags.NArg() != 0 {
		return errors.New("usage: clipctl paste-to --terminal ghostty")
	}
	return pasteToGhostty(terminal.GhosttyProvider{}, func(key string) (string, error) {
		return fetchPaste(socketPath, key, debug)
	})
}

func pasteToGhostty(provider terminal.GhosttyProvider, fetch func(string) (string, error)) error {
	session, err := provider.ActiveSession()
	if err != nil {
		return err
	}
	output, err := fetch(session.SessionKey)
	if err != nil {
		return err
	}
	if output == "" {
		return nil
	}
	return provider.PasteText(session, output)
}
