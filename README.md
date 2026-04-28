<div align="center">
  <img src="assets/brand/icon.svg" width="96" height="96" alt="clipport icon">
  <h1>clipport</h1>
  <p><strong>Paste macOS clipboard images into remote SSH and tmux sessions.</strong></p>
  <p><code>clipport paste-image</code></p>
</div>

`clipport` turns a local clipboard image into a file on the remote machine.

Run:

```bash
clipport paste-image
```

It uploads the current macOS clipboard image and prints exactly one thing: the
remote path.

```text
/tmp/clipport/<local-user>/<machine>/clipboard-20260428-132708.241851.png
```

Bind that command to an iTerm hotkey. When your cursor is in a remote shell, the
hotkey inserts the uploaded file path at the prompt.

## What it solves

Remote terminals cannot see your Mac clipboard. `clipport` bridges that gap for
images by using the thing SSH already gives you: a remote filesystem.

Copy an image locally. Paste a path remotely.

## Setup

### Prerequisites

- macOS
- iTerm2
- Homebrew
- passwordless SSH to each configured route
- writable `/tmp` on the remote machine

### Install

```bash
curl -fsSL https://raw.githubusercontent.com/arihantsethia/clipport/main/install.sh | sh
```

The installer:

- installs local dependencies with Homebrew
- builds `clipport` and `clipportd` into `~/.local/bin`
- runs onboarding
- can install SSH session matching
- starts the launchd daemon
- can add the iTerm hotkey

From a checkout:

```bash
./install.sh
```

To install somewhere else:

```bash
CLIPPORT_BIN=/some/bin ./install.sh
```

The default iTerm hotkey is `Cmd-Shift-V`. Override it with
`CLIPPORT_ITERM_KEY` if you already use that binding.

Install choices are saved in `~/.config/clipport/install.toml`. This file does
not contain secrets. `clipport uninstall` uses it later to remove the same
launchd, binary, SSH, and iTerm artifacts that install created.

### Check it

```bash
clipport doctor
clipport test-paste --host <machine>
clipport status
```

`test-paste` uploads an embedded PNG and verifies the remote file.

## Usage

### Paste path

```text
iTerm hotkey -> clipport paste-image -> clipportd -> SSH upload -> remote path
```

The path is printed to stdout and nothing else. This is what makes the command
safe to bind directly to a terminal hotkey.

### Remote shims

Most users do not need shims.

Use them only when a remote program calls `wl-paste` or `xclip` directly.

Set up the SSH remote forward and install the shim:

```bash
clipport shims setup --host <machine>
```

The shim token is stored on the remote at `~/.config/clipport/token` with
`0600` permissions.

### SSH session matching

The installer can add OpenSSH `LocalCommand` hooks for configured SSH aliases.
When you open `ssh <alias>` in iTerm, clipport records that iTerm session as
pointing at the logical machine. The next paste can then infer where to upload.

## Uninstall

Remove installed artifacts:

```bash
clipport uninstall
```

This removes the launchd service, launch agent, installed binaries, clipport
SSH config blocks, and the matching iTerm hotkey.

Useful uninstall options:

```bash
clipport uninstall --dry-run
clipport uninstall --remove-data
clipport uninstall --bin-dir ~/.local/bin
```

By default, uninstall keeps local config, cache, token, and temporary files.
`--remove-data` deletes them.

Remove remote shims and SSH `RemoteForward` blocks:

```bash
clipport shims uninstall --host <machine>
clipport shims uninstall --host <machine> --remove-remote-token
```

Remote uninstall keeps the remote token by default.

## Reference

### Benchmarks

Measured on 2026-04-28 from macOS to a remote machine over public SSH.

| Command | n | p50 | Notes |
|---|---:|---:|---|
| `clipport status` | 30 | 13 ms | local Unix socket |
| `clipport test-paste --host <machine>` | 10 | 157 ms | cached route, upload, verify |
| `test-paste` upload phase | 10 | 88 ms | SSH upload |
| `test-paste` verify phase | 10 | 70 ms | remote `test -f` |
| `clipport doctor` | 5 | 3.46 s | includes failed route probes |

### Troubleshooting

- `doctor` shows LAN failures: expected when off that network.
- Active session cannot be matched: run
  `clipport session register --machine <machine>` in the iTerm session.
- Hotkey fails silently: run `clipport doctor` and check `/tmp/clipportd.err.log`.

### Development

```bash
gofmt -w <changed go files>
go test ./...
go vet ./...
go build ./cmd/clipport ./cmd/clipportd
```

See [docs/architecture.md](docs/architecture.md) for the system design.
