#!/bin/sh
set -eu

repo_url="${CLIPPORT_REPO:-https://github.com/arihantsethia/clipport.git}"
repo_ref="${CLIPPORT_REF:-main}"
bin_dir="${CLIPPORT_BIN:-$HOME/.local/bin}"
config_path="${CLIPPORT_CONFIG:-$HOME/.config/clipport/config.toml}"
manifest_path="$HOME/.config/clipport/install.toml"
http_addr="${CLIPPORT_HTTP:-}"
label="com.clipport.clipportd"
plist_path="$HOME/Library/LaunchAgents/$label.plist"
iterm_key="${CLIPPORT_ITERM_KEY:-0x76-0x120000}"
iterm_configured=0
session_hooks_configured=0

tmp_dir=""

cleanup() {
  if [ -n "$tmp_dir" ]; then
    rm -rf "$tmp_dir"
  fi
}
trap cleanup EXIT INT TERM

say() {
  printf '%s\n' "$*"
}

die() {
  say "clipport: $*" >&2
  exit 1
}

toml_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

have() {
  command -v "$1" >/dev/null 2>&1
}

port_in_use() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

choose_http_addr() {
  if [ -n "$http_addr" ]; then
    return
  fi
  port=18765
  while [ "$port" -le 18865 ]; do
    if ! port_in_use "$port"; then
      http_addr="127.0.0.1:$port"
      return
    fi
    port=$((port + 1))
  done
  die "no free loopback HTTP port found in 18765-18865"
}

validate_http_addr() {
  case "$http_addr" in
    127.0.0.1:*|localhost:*) ;;
    *) die "CLIPPORT_HTTP must bind to loopback, got $http_addr" ;;
  esac
}

can_prompt() {
  [ -r /dev/tty ] && [ -w /dev/tty ] && { : </dev/tty >/dev/tty; } 2>/dev/null
}

confirm() {
  prompt="$1"
  default="${2:-y}"
  if ! can_prompt; then
    [ "$default" = "y" ]
    return
  fi
  if [ "$default" = "y" ]; then
    suffix="[Y/n]"
  else
    suffix="[y/N]"
  fi
  printf '%s %s ' "$prompt" "$suffix" >/dev/tty
  IFS= read -r answer </dev/tty || answer=""
  case "$answer" in
    y|Y|yes|YES) return 0 ;;
    n|N|no|NO) return 1 ;;
    "") [ "$default" = "y" ] ;;
    *) return 1 ;;
  esac
}

source_dir() {
  if [ -f "./go.mod" ] && [ -d "./cmd/clipport" ]; then
    pwd
    return
  fi
  case "$0" in
    */*)
      script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd || true)
      if [ -n "$script_dir" ] && [ -f "$script_dir/go.mod" ] && [ -d "$script_dir/cmd/clipport" ]; then
        say "$script_dir"
        return
      fi
      ;;
  esac
  have git || die "git is required"
  tmp_dir=$(mktemp -d)
  git clone --depth 1 --branch "$repo_ref" "$repo_url" "$tmp_dir/clipport"
  say "$tmp_dir/clipport"
}

write_launch_agent() {
  mkdir -p "$HOME/Library/LaunchAgents"
  cat >"$plist_path" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$bin_dir/clipportd</string>
    <string>--config</string>
    <string>$config_path</string>
    <string>--http</string>
    <string>$http_addr</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/tmp/clipportd.out.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/clipportd.err.log</string>
</dict>
</plist>
PLIST

  launchctl bootout "gui/$(id -u)" "$plist_path" >/dev/null 2>&1 || true
  launchctl bootstrap "gui/$(id -u)" "$plist_path"
  launchctl kickstart -k "gui/$(id -u)/$label"
}

configure_iterm() {
  case "${CLIPPORT_CONFIGURE_ITERM:-ask}" in
    0|false|no) return ;;
    1|true|yes) ;;
    ask)
      confirm "Configure iTerm hotkey for clipport paste?" y || return
      ;;
    *) die "CLIPPORT_CONFIGURE_ITERM must be 0, 1, yes, no, ask, or unset" ;;
  esac

  prefs="$HOME/Library/Preferences/com.googlecode.iterm2.plist"
  command_text="$bin_dir/clipport paste"
  mkdir -p "$(dirname "$prefs")"
  if [ ! -f "$prefs" ]; then
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8"?>' \
      '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
      '<plist version="1.0"><dict/></plist>' >"$prefs"
  fi

  /usr/libexec/PlistBuddy -c "Add :GlobalKeyMap dict" "$prefs" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Delete :GlobalKeyMap:$iterm_key" "$prefs" >/dev/null 2>&1 || true
  /usr/libexec/PlistBuddy -c "Add :GlobalKeyMap:$iterm_key dict" "$prefs"
  /usr/libexec/PlistBuddy -c "Add :GlobalKeyMap:$iterm_key:Action integer 35" "$prefs"
  /usr/libexec/PlistBuddy -c "Add :GlobalKeyMap:$iterm_key:Text string $command_text" "$prefs"

  iterm_configured=1
  say "configured iTerm hotkey: Cmd-Shift-V -> clipport paste"
  say "restart iTerm if it was open during install"
}

configure_session_hooks() {
  case "${CLIPPORT_CONFIGURE_SESSION_HOOKS:-ask}" in
    0|false|no) return ;;
    1|true|yes) ;;
    ask)
      confirm "Enable automatic SSH session matching?" y || return
      ;;
    *) die "CLIPPORT_CONFIGURE_SESSION_HOOKS must be 0, 1, yes, no, ask, or unset" ;;
  esac

  "$bin_dir/clipport" ssh install-session-hooks --config "$config_path" --clipport-bin "$bin_dir/clipport"
  session_hooks_configured=1
}

write_manifest() {
  mkdir -p "$(dirname "$manifest_path")"
  cat >"$manifest_path" <<MANIFEST
bin_dir = "$(toml_escape "$bin_dir")"
config_path = "$(toml_escape "$config_path")"
ssh_config_path = "$(toml_escape "$HOME/.ssh/config")"
launchd_plist_path = "$(toml_escape "$plist_path")"
http_addr = "$(toml_escape "$http_addr")"
iterm_key = "$(toml_escape "$iterm_key")"
iterm_configured = $(if [ "$iterm_configured" = 1 ]; then printf true; else printf false; fi)
session_hooks_configured = $(if [ "$session_hooks_configured" = 1 ]; then printf true; else printf false; fi)
MANIFEST
  chmod 600 "$manifest_path"
}

[ "$(uname -s)" = "Darwin" ] || die "install.sh currently supports macOS only"
have brew || die "Homebrew is required. Install it from https://brew.sh, then rerun install.sh."

if ! have pngpaste; then
  brew install pngpaste
fi
if ! have go; then
  brew install go
fi

choose_http_addr
validate_http_addr

src=$(source_dir)
cd "$src"

mkdir -p "$bin_dir"
build_dir=$(mktemp -d)
tmp_dir="${tmp_dir:-}"
trap 'rm -rf "$build_dir"; cleanup' EXIT INT TERM

go build -o "$build_dir/clipport" ./cmd/clipport
go build -o "$build_dir/clipportd" ./cmd/clipportd

install -m 0755 "$build_dir/clipport" "$bin_dir/clipport"
install -m 0755 "$build_dir/clipportd" "$bin_dir/clipportd"

if [ ! -f "$config_path" ]; then
  if can_prompt; then
    "$bin_dir/clipport" onboard --output "$config_path" </dev/tty >/dev/tty
  else
    die "no config found; rerun install.sh from an interactive terminal"
  fi
fi

configure_session_hooks
write_launch_agent
configure_iterm
write_manifest

case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) say "add to PATH: export PATH=\"$bin_dir:\$PATH\"" ;;
esac

say "installed $bin_dir/clipport"
say "installed $bin_dir/clipportd"
say "daemon $label"
say "wrote install manifest $manifest_path"
say "uninstall with: $bin_dir/clipport uninstall"
