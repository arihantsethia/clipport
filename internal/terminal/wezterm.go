package terminal

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WeztermSessionKey identifies a pane in one local GUI socket generation.
// Pane IDs alone are reused when WezTerm restarts. This deliberately requires
// the socket exported by WezTerm instead of discovering a different GUI.
func WeztermSessionKey(socketPath, paneID string) (string, error) {
	id, err := strconv.ParseUint(paneID, 10, 64)
	if err != nil || strconv.FormatUint(id, 10) != paneID {
		return "", fmt.Errorf("invalid WezTerm pane ID %q", paneID)
	}
	if !filepath.IsAbs(socketPath) {
		return "", fmt.Errorf("WEZTERM_UNIX_SOCKET must name a local absolute socket path")
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		return "", fmt.Errorf("inspect WezTerm socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return "", fmt.Errorf("WEZTERM_UNIX_SOCKET is not a socket")
	}
	generation := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", filepath.Clean(socketPath), info.ModTime().UnixNano())))
	return fmt.Sprintf("wezterm:%x:%s", generation, paneID), nil
}

// SessionKeyFromEnv is for commands running in the originating local shell,
// including OpenSSH LocalCommand. It never discovers a focused window.
func SessionKeyFromEnv(getenv func(string) string) (string, error) {
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "ghostty":
		return "", fmt.Errorf("Ghostty needs explicit pane registration: use session register --terminal ghostty --machine <name> before SSH")
	case "wezterm":
		return WeztermSessionKey(getenv("WEZTERM_UNIX_SOCKET"), getenv("WEZTERM_PANE"))
	default:
		return getenv("TERM_SESSION_ID"), nil
	}
}

// RequiresExplicitBinding excludes title and focused-window routing for the
// remote-only terminal integrations.
func RequiresExplicitBinding(key string) bool {
	key = strings.TrimSpace(key)
	return strings.HasPrefix(key, "ghostty:") || strings.HasPrefix(key, "wezterm:")
}
