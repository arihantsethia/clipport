package shims

import (
	"strings"
	"testing"
)

func TestShimUsesGenericClipboardAndDebugDiagnostics(t *testing.T) {
	shim := renderScript(18765)
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

func TestInstallRejectsUnsafeInputBeforeSSH(t *testing.T) {
	for _, token := range []string{"", "abc\nCLIPPORT_TOKEN\ntouch /tmp/not-allowed", "a'b", "$(id)", strings.Repeat("a", 4097)} {
		if err := Install("unused-target", token, 18765); err == nil || !strings.Contains(err.Error(), "URL-safe") {
			t.Fatalf("unsafe input not rejected locally: %v", err)
		}
	}
	for _, port := range []int{-1, 65536} {
		if err := Install("unused-target", "valid_token", port); err == nil || !strings.Contains(err.Error(), "port must") {
			t.Fatalf("invalid port not rejected locally: %v", err)
		}
	}
}
