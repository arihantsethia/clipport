<div align="center">
  <img src="assets/brand/icon.svg" width="96" height="96" alt="clipport icon">
  <h1>clipport</h1>
  <p><strong>Make iTerm paste work in remote shells.</strong></p>
  <p>
    <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
    <img alt="Go 1.26.1" src="https://img.shields.io/badge/go-1.26.1-00ADD8.svg">
    <img alt="macOS" src="https://img.shields.io/badge/platform-macOS-lightgrey.svg">
    <img alt="iTerm2" src="https://img.shields.io/badge/terminal-iTerm2-5319e7.svg">
  </p>
</div>

Clipport sits behind an iTerm key binding. Copy text or an image on your Mac,
press the paste shortcut in iTerm, and Clipport chooses the right behavior for
the active session.

- Local shell: native paste.
- Remote shell, text copied: paste the text.
- Remote shell, image copied: upload the image over SSH and paste the remote
  path.

Example remote image path:

```text
/tmp/clipport/yourname/clipboard-20260428-132708.241851.png
```

This is useful when the thing consuming the paste is not on your Mac: a VM,
remote workstation, cloud box, or coding-agent harness. The remote process
cannot read your Mac clipboard, but it can read a file path on its own
filesystem.

## Why

Use Clipport when you need to send local context into a remote terminal:

- paste screenshots into an agent session running on a VM
- hand a design reference to a remote coding session
- avoid manual `scp` for one-off clipboard images
- keep one paste shortcut for local and SSH shells

## Install

Requirements:

- macOS
- iTerm2
- Homebrew
- passwordless SSH to each remote machine
- writable `/tmp` on each remote machine

Install:

```bash
curl -fsSL https://raw.githubusercontent.com/arihantsethia/clipport/main/install.sh | sh
```

The installer builds `clipport` and `clipportd`, runs onboarding, starts the
launchd daemon, and can bind `Cmd-Shift-V` in iTerm.

If `~/.local/bin` is not on your `PATH`, the installer prints the export line to
add.

## First Check

```bash
clipport doctor
```

Then open an SSH session in iTerm, copy text on your Mac, and press the
Clipport key binding. Text should appear at the remote prompt.

For images, copy an image or screenshot and press the same key binding. The
remote prompt should receive a path like:

```text
/tmp/clipport/yourname/clipboard-20260428-132708.241851.png
```

Test the remote upload path without changing your clipboard:

```bash
clipport test-paste --host <machine>
```

## Agent Example

```text
You: copy a screenshot on the Mac
You: press Cmd-Shift-V in the SSH session
Shell: /tmp/clipport/yourname/clipboard-20260428-132708.241851.png
You: "Open that image and fix the layout issue."
```

The agent sees a normal file path on the remote machine. You do not need to
start a file server, drag files around, or change the app being inspected.

## Paste Behavior

iTerm runs this command behind the key binding:

```bash
clipport paste
```

Stdout is strict because iTerm inserts stdout into the terminal:

| Session | Clipboard | Output |
|---|---|---|
| local | text or image | nothing; native Cmd-V is sent |
| remote | text | clipboard text |
| remote | image | `/tmp/clipport/...` path only |

## Configuration

Main config:

```text
~/.config/clipport/config.toml
```

A machine is one remote filesystem. A route is one SSH path to it.

```toml
default_host = "devbox"

[[hosts]]
name = "devbox"
match_hosts = ["devbox", "devbox.example.com"]

[[hosts.routes]]
name = "lan"
ssh_target = "devbox-lan"
priority = 10

[[hosts.routes]]
name = "public"
ssh_target = "devbox-public"
priority = 20
```

Routes are tried by ascending `priority`. `ssh_target` is passed to OpenSSH, so
your existing `~/.ssh/config` settings still apply.

Rerun onboarding:

```bash
clipport onboard
```

List SSH hosts seen by onboarding:

```bash
clipport onboard --list
```

## Session Matching

Clipport needs to map the active iTerm session to a configured machine. The
installer can add OpenSSH `LocalCommand` hooks to do this automatically.

Register the current iTerm session manually:

```bash
clipport session register --machine <machine>
```

Show configured hosts, registered sessions, and recent transfers:

```bash
clipport status
```

## Commands

```bash
# Usually hidden behind the iTerm key binding.
clipport paste

# Health check.
clipport doctor

# Upload an embedded test PNG to a configured machine.
clipport test-paste --host <machine>

# Status and recent transfers.
clipport status
```

Debug paste failures:

```bash
clipport --debug paste
```

## Remote Clipboard Shims

Most users do not need shims. They are only for remote programs that call
`wl-paste` or `xclip` directly.

Install shims for a configured machine:

```bash
clipport shims setup --host <machine>
```

Both local and remote tokens live at:

```text
~/.config/clipport/token
```

Permissions are `0600`. Tokens are not embedded in executable scripts. The
local HTTP endpoint binds only to loopback.

Remove shims:

```bash
clipport shims uninstall --host <machine>
```

Remove the remote token too:

```bash
clipport shims uninstall --host <machine> --remove-remote-token
```

## Install Options

From a checkout:

```bash
./install.sh
```

Install binaries somewhere else:

```bash
CLIPPORT_BIN=/some/bin ./install.sh
```

Use a different iTerm key binding:

```bash
CLIPPORT_ITERM_KEY=<iterm-key-code> ./install.sh
```

Skip optional iTerm or SSH hook setup:

```bash
CLIPPORT_CONFIGURE_ITERM=no ./install.sh
CLIPPORT_CONFIGURE_SESSION_HOOKS=no ./install.sh
```

Install choices are saved in:

```text
~/.config/clipport/install.toml
```

The manifest contains no secrets. `clipport uninstall` uses it to remove the
same launchd, binary, SSH, and iTerm artifacts created by install.

## Uninstall

```bash
clipport uninstall
```

Preview:

```bash
clipport uninstall --dry-run
```

Remove local config, cache, token, and temporary files:

```bash
clipport uninstall --remove-data
```

By default, uninstall keeps local data and remote shim tokens.

## Troubleshooting

Start with:

```bash
clipport doctor
```

Common cases:

- LAN route fails: expected when off that network; another route can still work.
- SSH shell gets normal paste: run
  `clipport session register --machine <machine>` in that iTerm session.
- Hotkey does nothing: confirm iTerm runs `clipport paste`, then check
  `/tmp/clipportd.err.log`.
- Image upload fails: check passwordless SSH and remote write access to
  `/tmp/clipport`.

## Development

```bash
gofmt -w <changed go files>
go test ./...
go vet ./...
go build ./cmd/clipport ./cmd/clipportd
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for the contributor architecture map.

## License

Clipport is released under the [MIT License](LICENSE).
