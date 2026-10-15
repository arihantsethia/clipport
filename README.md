<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/brand/icon.svg">
    <source media="(prefers-color-scheme: light)" srcset="assets/brand/dark_icon.png">
    <img src="assets/brand/icon.svg" width="96" height="96" alt="clipport icon">
  </picture>
  <h1>clipport</h1>
  <p><strong>Paste Mac clipboard text, images, and screenshots into remote iTerm SSH sessions.</strong></p>
  <p>
    <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue.svg"></a>
    <img alt="Go 1.26.2" src="https://img.shields.io/badge/go-1.26.2-00ADD8.svg">
    <img alt="macOS" src="https://img.shields.io/badge/platform-macOS-lightgrey.svg">
    <img alt="iTerm2" src="https://img.shields.io/badge/terminal-iTerm2-5319e7.svg">
  </p>
</div>

Clipport lets you share images and screenshots over SSH without saving files by
hand or running one-off `scp` commands. One iTerm paste shortcut works across
local and remote shells: Clipport detects the active session, reads the Mac
clipboard, and chooses the right paste behavior.

If your Mac clipboard has text, the remote shell receives text. If it has an
image or screenshot, Clipport uploads it over SSH and inserts the remote path.

| Session | Clipboard | Terminal receives |
|---|---|---|
| local | text, image, or screenshot | native paste; `clipctl paste` prints nothing |
| configured remote | text | clipboard text |
| configured remote | screenshot or image | remote file path under `/tmp/clipport/...` |
| unconfigured remote | any | session-matching error; use native paste instead |

Keep **Cmd+V** for native paste and use **Cmd+Shift+V** for Clipport. The current
local-paste implementation simulates Cmd+V, so binding Clipport to Cmd+V can
trigger itself repeatedly. Remote sessions that cannot be matched do not fall
back to native paste.

## Demo

![Clipport demo](assets/demo.gif)

Remote image or screenshot example:

```text
/tmp/clipport/yourname/clipboard-20260428-132708.241851.png
```

Use Clipport when a remote process needs the thing on your Mac clipboard: a
screenshot, an image, or plain text pasted through the same terminal shortcut.

## Use Cases

Clipport is useful when the remote shell needs local clipboard state:

- paste a screenshot path into a VM shell
- hand a local image to a remote command
- avoid one-off `scp` for screenshots and clipboard images
- keep one paste shortcut for local and SSH shells

## Quick Start

You need macOS, iTerm2, Homebrew, and an SSH alias in `~/.ssh/config` that
already connects without an interactive password. The remote SSH account must
be able to create private files under `/tmp/clipport`; see [Upload permissions](#upload-permissions).

Run these commands in a **local Mac shell**, outside SSH:

```bash
brew install arihantsethia/tap/clipport
clipctl onboard
```

Select your SSH hosts and answer **Y** to the iTerm shortcut and automatic
session-matching prompts. Homebrew installs the CLI, daemon, and menu bar app;
onboarding records their paths, starts or restarts the app, and waits for the
daemon and authenticated HTTP health check. If startup fails, onboarding reports
an error instead of completing; see [Troubleshooting](#troubleshooting).

Open a **new SSH connection** to a selected alias in iTerm so the session hook
runs. Copy text, then a screenshot, on your Mac and press **Cmd+Shift+V** in the
SSH session. Text is pasted directly; an image is uploaded and its remote file
path is inserted. Keep **Cmd+V** for native paste, including unconfigured servers.

[Remote clipboard shims](#remote-clipboard-shims) are optional and are not needed
for this shortcut. If pasting fails, see [Troubleshooting](#troubleshooting).

## Verify the shortcut and upload

Run this **on your Mac**, replacing `<machine>` with the name chosen during
onboarding:

```bash
clipctl test-paste --host <machine>
```

This uploads an embedded test image without changing your clipboard. It checks
the upload path, not the iTerm shortcut or the receiving application's ability
to open an image. A pasted path is not a guaranteed image attachment in every
remote application.

### Check the iTerm shortcut

Open **iTerm2 → Settings → Keys → Key Bindings** and inspect **Cmd+Shift+V**.
Its command should be the absolute path to your installed `clipctl`, followed
by `paste`, for example:

```text
/Users/yourname/.local/bin/clipctl paste
```

Run `command -v clipctl` on your Mac to check the installed path. Also inspect
**Profiles → your active profile → Keys → Key Mappings**: a profile binding can
override the global shortcut. `clipctl doctor` finding a saved binding does not
prove that the active profile invokes it.

Keep Cmd+V assigned to native paste. Use it for servers you have not configured
in Clipport; the Clipport shortcut reports an error for an unmatched remote
session. Local Clipport paste simulates Cmd+V and therefore requires Cmd+V to
remain a native-paste shortcut.

## Behavior

iTerm runs this command behind the paste shortcut:

```bash
clipctl paste
```

Stdout is strict because iTerm inserts stdout into the terminal:

- local paste prints nothing
- remote text prints the clipboard text
- remote image upload prints only the remote path

Remote uploads always use paths under:

```text
/tmp/clipport/<local-user>/
```

### Upload permissions

Uploads create files with mode `0600` inside `0700` directories. Existing upload
directories owned by the remote SSH account are tightened before writing new
content. `/tmp/clipport` must be absent or owned by that account; symlinked or
foreign-owned directories and existing destination files are rejected. Existing
files are not overwritten or retroactively changed. A server where another
account owns `/tmp/clipport` needs an administrator-reviewed directory layout;
do not loosen permissions to bypass this check.

## Configuration

A machine is one remote filesystem. A route is one SSH path to that machine.
OpenSSH still owns aliases, keys, proxies, and connection behavior.

Rerun onboarding:

```bash
clipctl onboard
```

List SSH hosts seen by onboarding:

```bash
clipctl onboard --list
```

Local install settings and host routes live in:

```text
~/.config/clipport/config.toml
```

The file contains no secrets.

## Session Matching

Clipport maps the active iTerm session to a configured machine. OpenSSH
`LocalCommand` hooks make that mapping explicit. If hooks are not installed or
do not fire, terminal title detection and manual registration remain available.

For normal use, let the SSH `LocalCommand` hook register the pane when you
connect. To register manually, run this in the pane's **local Mac shell before
opening SSH**, not at the remote prompt:

```bash
clipctl session register --machine <machine>
```

Show configured hosts, registered sessions, and recent transfers:

```bash
clipctl status
```

## Menu Bar

When started, the menu bar app supervises `clipportd`. It shows daemon health,
configured hosts, and shortcuts to restart Clipport, run `clipctl doctor`, and
open config or log files.

`clipportd` exits when the menu bar app exits. Choosing `Quit` stops the
daemon and exits Clipport. The menu bar app does not change the
`clipctl paste` stdout contract.

## Commands

```bash
# Paste entrypoint used by iTerm.
clipctl paste

# Start, stop, or restart the menu bar app and daemon.
clipctl start
clipctl stop
clipctl restart

# Health check.
clipctl doctor

# Status and recent transfers.
clipctl status

# Upload an embedded test PNG to a configured machine.
clipctl test-paste --host <machine>

# Update manual/source installs only; Homebrew users should see Update below.
clipctl update
```

Debug paste failures:

```bash
clipctl --debug paste
```

## Remote Clipboard Shims

This optional mode lets programs on a trusted remote account read your Mac
clipboard while an SSH tunnel is open. All `clipctl` commands below run **on
your Mac**; do not install `clipctl` on the server.

Shims are only for remote programs that call `wl-paste` or `xclip` directly.
Ordinary Clipport shortcut pasting does not require them. Onboarding installs
session-matching hooks, but does **not** install shims, a `RemoteForward` rule,
or the remote authentication token.

For remote clipboard access, run this **on your Mac** for each configured machine
(replace `<machine>` with its name from onboarding):

```bash
clipctl shims setup --host <machine>
```

The server needs `curl`, and `~/.local/bin` must come before system clipboard
programs in the remote application's `PATH`. Setup installs wrappers there; it
does not modify your remote shell's `PATH`.

For each route, setup installs the remote wrappers and token before adding its
local SSH forwarding rule. An existing remote token must match this Mac's token;
a different or malformed token stops setup without replacing the token or
wrappers. Setup also refuses to replace unrelated clipboard commands. That token
allows remote clipboard access while the tunnel is connected.

Reconnect SSH using one of the configured route aliases and leave that session
open. Then run `clipctl doctor` in a local Mac pane. The `forward` check should
now pass. Reconnecting without first installing the forwarding rule and token
will not fix a missing tunnel.

In the **remote shell**, check which programs the application will find:

```bash
command -v curl
command -v wl-paste
command -v xclip
```

The clipboard commands should resolve to `~/.local/bin`. Copy harmless text on
your Mac, then run `wl-paste` in the remote shell to test the tunnel.

These are limited wrappers: a successful request returns the current clipboard
bytes and does not implement `wl-paste`/`xclip` options such as type enumeration
or selection handling. If the tunnel fails, they try a system clipboard program
with the original arguments. Test your application's clipboard integration
rather than assuming compatibility.

### Shared servers and clipboard access

Simultaneous remote-shim use from multiple clients is not supported. The remote
account has one token and one wrapper installation, and the server's loopback
forwarding port can already belong to another SSH connection. Separate remote
accounts alone do not prevent a port collision. Ordinary shortcut image uploads
do not use this clipboard tunnel.

A same-client rerun can repair its wrappers. If setup reports a different token,
stop and identify the client that owns it; there is no force-overwrite option.
A later-route or local-config failure can leave earlier work completed: follow
the reported partial-completion details before retrying.

Only enable this mode for accounts you trust with your clipboard contents.
Both local and remote tokens live at:

```text
~/.config/clipport/token
```

Permissions are `0600`. Tokens are not embedded in executable scripts. The
local HTTP endpoint binds only to loopback.

Remove shims:

```bash
clipctl shims uninstall --host <machine>
```

Remove the remote token too:

```bash
clipctl shims uninstall --host <machine> --remove-remote-token
```

## Source Install Options

The source installer builds from `main` by default, including unreleased changes.
It still requires Homebrew. For the tested stable release, use the Homebrew quick
start above. To install from source **on your Mac**:

```bash
curl -fsSL https://raw.githubusercontent.com/arihantsethia/clipport/main/install.sh | sh
```

Then run `clipctl onboard`. From a checkout:

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

Use a specific loopback HTTP address:

```bash
CLIPPORT_HTTP=127.0.0.1:18800 ./install.sh
```

## Update

For Homebrew installs, use Homebrew to install the latest published stable release:

```bash
brew update
brew upgrade arihantsethia/tap/clipport
clipctl restart
```

The tap checks for new releases hourly and updates only after macOS source and
formula tests pass. GitHub may delay scheduled runs. Maintainers can also run
[Update Clipport](https://github.com/arihantsethia/homebrew-tap/actions/workflows/update-clipport.yml)
manually. Each stable version has a versioned source URL and SHA-256 checksum.

For unreleased development changes, opt in with
`brew install --HEAD arihantsethia/tap/clipport`. Refresh an existing HEAD install
with `brew upgrade --fetch-HEAD arihantsethia/tap/clipport`, then `clipctl restart`.

For manual/source installs only:

```bash
clipctl update
```

`clipctl update` downloads the latest installer and applies it using the current
install settings.

Set `CLIPPORT_REF` to update from another branch or ref:

```bash
CLIPPORT_REF=<ref> clipctl update
```

## Uninstall

```bash
clipctl uninstall
```

Preview:

```bash
clipctl uninstall --dry-run
```

Remove local config, cache, token, and `/tmp/clipport` files:

```bash
clipctl uninstall --remove-data
```

By default, uninstall keeps local data and remote shim tokens.

## Troubleshooting

Run diagnostics **on your Mac**, outside SSH:

```bash
clipctl doctor
```

Doctor labels an unconfigured optional clipboard tunnel `skip`; shortcut paste
does not need it. A configured tunnel receives a real health check, so a broken
tunnel or an SSH-config inspection error still reports `fail`. Read the report:
failed checks do not currently change the command's exit status.

Common cases:

- `onboarding incomplete`: configuration was saved, but app startup or readiness
  failed. Inspect `/tmp/clipport.err.log` and `/tmp/clipportd.err.log`, fix the
  reported problem, and rerun `clipctl onboard`. Startup and readiness have a
  15-second deadline. Do not run Clipport as root to bypass a launchd error.
- `daemon` or `http api` reports connection refused: the app may still be
  starting. If it persists after a restart, inspect `/tmp/clipport.err.log` and
  `/tmp/clipportd.err.log`. An `ok launchd` result only confirms the agent is
  loaded, not that the daemon is healthy.
- `forward` reports connection refused: if you need remote clipboard shims,
  run `clipctl shims setup --host <machine>` on your Mac, then reconnect SSH and
  keep the connection open. Use `ssh -G <ssh-alias>` locally to check that the
  effective configuration contains `remoteforward`. The default forwarded
  endpoint is `127.0.0.1:18765`.
- `forward` reports `401 Unauthorized`: the endpoint is reachable, but the
  remote token does not authenticate to it. Check whether another client owns
  the tunnel or remote token before changing anything. Do not overwrite another
  client's token to make this check pass. See [Shared servers and clipboard access](#shared-servers-and-clipboard-access).
- An SSH session hook conflicts with existing settings: onboarding repairs its
  own managed hook, but will not overwrite a user-defined `LocalCommand` or
  a conflicting `PermitLocalCommand`. Inspect `ssh -G <ssh-alias>` on your Mac,
  resolve the reported conflict in your SSH config, and rerun onboarding before
  reconnecting.
- LAN route fails: expected when off that network; Clipport tries the next
  route.
- The SSH pane is matched incorrectly: check its local SSH session hook and
  reconnect. See [Session Matching](#session-matching) for the advanced local-Mac
  registration command; running it at the remote prompt will not register the
  Mac's pane.
- Hotkey does nothing: follow [Check the iTerm shortcut](#check-the-iterm-shortcut),
  then check `/tmp/clipportd.err.log`.
- Image upload fails: check passwordless SSH and remote write access to
  `/tmp/clipport`.

## Development

```bash
gofmt -w <changed go files>
go test ./...
go vet ./...
go build ./cmd/clipctl ./cmd/clipportd
CGO_ENABLED=1 go build -o /tmp/clipport-check ./cmd/clipport
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for the contributor architecture map.
Report security issues using [SECURITY.md](SECURITY.md).

## License

Clipport is released under the [MIT License](LICENSE).

## Releases

See [RELEASING.md](RELEASING.md) for the tested tag-to-release and Homebrew update process.
