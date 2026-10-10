# Ghostty and WezTerm (macOS)

These integrations add a **separate remote-paste shortcut**. Keep the terminal's
normal Cmd+V for local paste. Clipport sends text or uploads an image and pastes
its remote path; it does not append Enter or change the Mac clipboard.

Configure your hosts with `clipctl onboard` (decline the iTerm hotkey prompt if
you do not use iTerm), then `clipctl restart`. Both CLI and daemon must include
this feature. Older daemons reject the new bound-only request instead of routing
it through an unrelated iTerm window.

## Ghostty 1.3+

Ghostty's AppleScript terminal UUID is different from `GHOSTTY_SURFACE_ID`.
Clipport deliberately does not infer a pane from environment variables, titles,
or working directories.

1. Focus the intended Ghostty pane. **In its local shell, before starting SSH**,
   explicitly register the configured Clipport machine:

   ```sh
   clipctl session register --terminal ghostty --machine devbox
   ssh devbox
   ```

   Repeat registration whenever that pane changes machines. Do not run it from
   the remote shell. Registration explicitly selects the currently focused pane,
   so keep that pane focused until the command completes.

2. In macOS Shortcuts, create a shortcut with a **Run Shell Script** action:

   ```sh
   /opt/homebrew/bin/clipctl --debug paste-to --terminal ghostty
   ```

   Use the actual absolute path from `command -v clipctl` (Intel Homebrew normally
   uses `/usr/local/bin/clipctl`). Assign a keyboard shortcut such as Cmd+Shift+V.
   Invoke it while Ghostty is frontmost. Approve macOS Automation access to
   Ghostty when prompted; `macos-applescript` must be enabled.

3. Copy text or an image, then invoke that shortcut. It captures the pane once,
   requests the payload for its explicit binding, and delivers to that UUID even
   if you focus a different pane while uploading. Missing/closed panes fail; no
   other pane is selected. Delivery uses a temporary `0600` UTF-8 file, removed
   afterward, so clipboard text is not placed in process arguments or scripts.

Bindings include the Ghostty process launch identity because window restoration
can reuse a terminal UUID. Re-register after restarting Ghostty. The 1.3 API
cannot verify that the original SSH process is still running: use this shortcut
only in the SSH session you registered. After leaving SSH, use ordinary Cmd+V;
a saved binding is not a live-session detector. Ghostty's OpenSSH hook cannot
safely infer the originating pane on this API, so automatic registration reports
that explicit registration is needed.

The AppleScript API is a preview API. This implementation targets the documented
1.3 object model; unsupported or disabled scripting fails rather than switching
to another terminal. No terminal preferences are edited automatically.

## WezTerm

Requires macOS WezTerm **20230408-112425-69ae8472 or newer**, a local pane, and
OpenSSH running as that pane's foreground process. WezTerm SSH/mux domains,
serial panes, and SSH hidden behind tmux are not supported by this integration.

1. Install or update the OpenSSH session hooks:

   ```sh
   clipctl ssh install-session-hooks
   ```

   Existing Clipport hooks with the old iTerm-only environment argument are
   upgraded with a backup; unrelated SSH config is preserved. Start a new SSH
   connection afterward. `LocalCommand` records the originating shell's
   `WEZTERM_PANE` and local `WEZTERM_UNIX_SOCKET` generation, not whichever pane
   happens to be focused. You can also register explicitly in a local shell:

   ```sh
   clipctl session register --terminal wezterm --machine devbox
   ssh devbox
   ```

2. Copy [`wezterm.lua`](wezterm.lua) next to your WezTerm configuration as
   `clipport.lua`. Adjust its absolute `clipctl` path. Add its returned keybinding
   to your existing `config.keys`, keeping any existing bindings:

   ```lua
   config.keys = config.keys or {}
   table.insert(config.keys, require 'clipport')
   ```

3. OpenSSH to the registered machine and press Cmd+Shift+V. The callback keeps
   the original pane object throughout the upload. It checks that the same
   foreground SSH process is still there before delivering; on failure, it shows
   a notification and pastes nothing. It never selects the first `is_active`
   entry from `wezterm cli list` (multiple tabs can have active panes).

Bare pane IDs reset on restart. Clipport qualifies them with the local socket
path and its creation-time modification timestamp. This is an application-level
freshness check, not a persistent identity API. A missing/recreated socket fails
or requires registration again; Clipport does not search for a replacement GUI.

## Validation

Automated tests cover pane-key isolation, restart detection, focus changes,
private payload cleanup, failed uploads, closed panes, SSH exit/replacement,
stdout preservation, and legacy SSH-hook migration. The Lua tests run the actual
callback with mocked WezTerm APIs:

```sh
lua integrations/wezterm_test.lua
```

A live smoke test still needs both apps, Automation permission, and an SSH host:
text with quotes and trailing newlines; an image; two panes on different hosts;
focus changes during upload; closing a pane; restarting the app; and SSH exit.
Unit tests and macOS CI do not establish that this live test passed.

## API references

- [Ghostty AppleScript](https://ghostty.org/docs/features/applescript)
- [Ghostty 1.3 scripting dictionary](https://github.com/ghostty-org/ghostty/blob/v1.3.1/macos/Ghostty.sdef)
- [WezTerm local TTY identity](https://wezterm.org/config/lua/pane/get_tty_name.html)
- [WezTerm foreground process](https://wezterm.org/config/lua/pane/get_foreground_process_info.html)
- [WezTerm child process](https://wezterm.org/config/lua/wezterm/run_child_process.html)
- [WezTerm targeted paste](https://wezterm.org/config/lua/pane/send_paste.html)
