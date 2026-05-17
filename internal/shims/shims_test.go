package shims

import (
	"strings"
	"testing"
)

func TestInstallPayloadKeepsTokenOutOfExecutableShim(t *testing.T) {
	payload := renderInstallPayload("secret-token", 18765)
	if !strings.Contains(payload, "chmod 600 ~/.config/clipport/token") {
		t.Fatalf("payload does not chmod token file:\n%s", payload)
	}
	start := strings.Index(payload, "cat > ~/.local/bin/clipport-wl-paste")
	if start < 0 {
		t.Fatalf("payload does not write shim:\n%s", payload)
	}
	shim := payload[start:]
	if strings.Contains(shim, "secret-token") {
		t.Fatalf("shim embeds token:\n%s", shim)
	}
	if !strings.Contains(shim, "cat \"$HOME/.config/clipport/token\"") {
		t.Fatalf("shim does not read token file:\n%s", shim)
	}
	if !strings.Contains(shim, "/v1/clipboard") || strings.Contains(shim, "/v1/clipboard/png") {
		t.Fatalf("shim does not use generic clipboard endpoint:\n%s", shim)
	}
	if !strings.Contains(shim, "2>\"$curl_stderr\"") || !strings.Contains(shim, "CLIPPORT_DEBUG") {
		t.Fatalf("shim does not gate curl stderr behind debug:\n%s", shim)
	}
}

func TestShimFallbackAvoidsLocalBinRecursion(t *testing.T) {
	shim := renderScript(18765)
	if !strings.Contains(shim, "PATH_WITHOUT_SHIMS") {
		t.Fatalf("shim does not remove ~/.local/bin from PATH:\n%s", shim)
	}
	if strings.Contains(shim, "/usr/bin/wl-paste") || strings.Contains(shim, "/usr/bin/xclip") {
		t.Fatalf("shim hard-codes fallback binary paths:\n%s", shim)
	}
}

func TestUninstallPayloadRemovesOnlyClipportShimSymlinks(t *testing.T) {
	payload := renderUninstallPayload(false)
	for _, want := range []string{
		"for name in wl-paste xclip; do",
		"if [ -L \"$link\" ]; then",
		"readlink \"$link\"",
		"rm -f \"$link\"",
		"rm -f \"$shim\"",
	} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q:\n%s", want, payload)
		}
	}
	if strings.Contains(payload, "rm -f \"$HOME/.config/clipport/token\"") {
		t.Fatalf("payload removes token without opt-in:\n%s", payload)
	}
}

func TestUninstallPayloadRemovesTokenOnlyWhenRequested(t *testing.T) {
	payload := renderUninstallPayload(true)
	if !strings.Contains(payload, "rm -f \"$HOME/.config/clipport/token\"") {
		t.Fatalf("payload does not remove token when requested:\n%s", payload)
	}
}

func TestShimInstallSSHCommandDoesNotRequestForwarding(t *testing.T) {
	args := strings.Join(sshShCommand("devbox").Args, "\n")
	for _, want := range []string{
		"-o\nPermitLocalCommand=no",
		"-o\nClearAllForwardings=yes",
		"devbox",
		"sh",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("ssh args missing %q in:\n%s", want, args)
		}
	}
}
