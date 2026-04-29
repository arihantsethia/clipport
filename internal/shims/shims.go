package shims

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/arihantsethia/clipport/internal/registry"
)

const Version = "v0.1.0"

func Install(target, token string, port int) error {
	if target == "" {
		return fmt.Errorf("target is required")
	}
	if token == "" {
		return fmt.Errorf("token is required")
	}
	if port == 0 {
		port = 18765
	}
	payload := renderInstallPayload(token, port)
	cmd := exec.Command("ssh", target, "sh")
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install shims on %s failed: %w: %s", target, err, strings.TrimSpace(string(out)))
	}
	_ = markShim(target)
	return nil
}

func Uninstall(target string, removeToken bool) error {
	if target == "" {
		return fmt.Errorf("target is required")
	}
	payload := renderUninstallPayload(removeToken)
	cmd := exec.Command("ssh", target, "sh")
	cmd.Stdin = strings.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("uninstall shims on %s failed: %w: %s", target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func renderInstallPayload(token string, port int) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "set -eu\n")
	fmt.Fprintf(&b, "mkdir -p ~/.local/bin ~/.config/clipport\n")
	fmt.Fprintf(&b, "cat > ~/.config/clipport/token <<'CLIPPORT_TOKEN'\n%s\nCLIPPORT_TOKEN\n", token)
	fmt.Fprintf(&b, "chmod 600 ~/.config/clipport/token\n")
	fmt.Fprintf(&b, "cat > ~/.local/bin/clipport-wl-paste <<'CLIPPORT_SHIM'\n%sCLIPPORT_SHIM\n", renderScript(port))
	fmt.Fprintf(&b, "chmod 755 ~/.local/bin/clipport-wl-paste\n")
	fmt.Fprintf(&b, "ln -sf ~/.local/bin/clipport-wl-paste ~/.local/bin/wl-paste\n")
	fmt.Fprintf(&b, "ln -sf ~/.local/bin/clipport-wl-paste ~/.local/bin/xclip\n")
	return b.String()
}

func renderUninstallPayload(removeToken bool) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "set -eu\n")
	fmt.Fprintf(&b, "shim=\"$HOME/.local/bin/clipport-wl-paste\"\n")
	fmt.Fprintf(&b, "for name in wl-paste xclip; do\n")
	fmt.Fprintf(&b, "  link=\"$HOME/.local/bin/$name\"\n")
	fmt.Fprintf(&b, "  if [ -L \"$link\" ]; then\n")
	fmt.Fprintf(&b, "    target=$(readlink \"$link\" || true)\n")
	fmt.Fprintf(&b, "    case \"$target\" in\n")
	fmt.Fprintf(&b, "      \"$shim\"|\"$HOME/.local/bin/clipport-wl-paste\"|~/.local/bin/clipport-wl-paste) rm -f \"$link\" ;;\n")
	fmt.Fprintf(&b, "    esac\n")
	fmt.Fprintf(&b, "  fi\n")
	fmt.Fprintf(&b, "done\n")
	fmt.Fprintf(&b, "rm -f \"$shim\"\n")
	if removeToken {
		fmt.Fprintf(&b, "rm -f \"$HOME/.config/clipport/token\"\n")
	}
	return b.String()
}

func renderScript(port int) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "#!/bin/sh\n")
	fmt.Fprintf(&b, "set -eu\n")
	fmt.Fprintf(&b, "CLIPPORT_PORT=${CLIPPORT_PORT:-%d}\n", port)
	fmt.Fprintf(&b, "CLIPPORT_TOKEN=${CLIPPORT_TOKEN:-$(cat \"$HOME/.config/clipport/token\" 2>/dev/null || true)}\n")
	fmt.Fprintf(&b, "url=\"http://127.0.0.1:${CLIPPORT_PORT}/v1/clipboard\"\n")
	fmt.Fprintf(&b, "curl_stderr=/dev/null\n")
	fmt.Fprintf(&b, "if [ \"${CLIPPORT_DEBUG:-}\" = \"1\" ]; then curl_stderr=/dev/stderr; fi\n")
	fmt.Fprintf(&b, "if [ -n \"$CLIPPORT_TOKEN\" ] && command -v curl >/dev/null 2>&1 && curl -fsS -H \"Authorization: Bearer ${CLIPPORT_TOKEN}\" \"$url\" 2>\"$curl_stderr\"; then exit 0; fi\n")
	fmt.Fprintf(&b, "name=$(basename \"$0\")\n")
	fmt.Fprintf(&b, "PATH_WITHOUT_SHIMS=$(printf '%%s' \"$PATH\" | awk -v RS=: -v ORS=: '$0 != ENVIRON[\"HOME\"] \"/.local/bin\" {print}' | sed 's/:$//')\n")
	fmt.Fprintf(&b, "real=$(PATH=\"$PATH_WITHOUT_SHIMS\" command -v \"$name\" 2>/dev/null || true)\n")
	fmt.Fprintf(&b, "if [ -n \"$real\" ]; then exec \"$real\" \"$@\"; fi\n")
	fmt.Fprintf(&b, "echo \"clipport: clipboard tunnel unavailable and no fallback $name found\" >&2\n")
	fmt.Fprintf(&b, "exit 127\n")
	return b.String()
}

func markShim(target string) error {
	reg, err := registry.Load("")
	if err != nil {
		return err
	}
	reg.UpdateHost(target, func(st registry.HostState) registry.HostState {
		st.ShimVersion = Version
		return st
	})
	return reg.Save("")
}
