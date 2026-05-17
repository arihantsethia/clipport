#!/bin/sh
set -eu

repo_url="${CLIPPORT_REPO:-https://github.com/arihantsethia/clipport.git}"
repo_ref="${CLIPPORT_REF:-main}"
bin_dir="${CLIPPORT_BIN:-$HOME/.local/bin}"
config_path="${CLIPPORT_CONFIG:-$HOME/.config/clipport/config.toml}"
http_addr="${CLIPPORT_HTTP:-}"
app_label="com.clipport.app"
app_plist_path="$HOME/Library/LaunchAgents/$app_label.plist"
app_path="$HOME/Applications/Clipport.app"
iterm_key="${CLIPPORT_ITERM_KEY:-}"

tmp_dir=""
config_exists=0
app_was_running=0
install_result="installed"

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

have() {
  command -v "$1" >/dev/null 2>&1
}

port_in_use() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

app_launch_agent_running() {
  launchctl print "gui/$(id -u)/$app_label" >/dev/null 2>&1
}

app_process_running() {
  pgrep -f "$app_path/Contents/MacOS/clipport" >/dev/null 2>&1 ||
    pgrep -f "$bin_dir/clipportd" >/dev/null 2>&1
}

detect_install_action() {
  if [ -f "$config_path" ] || [ -x "$bin_dir/clipctl" ] || [ -d "$app_path" ] || [ -f "$app_plist_path" ]; then
    install_result="upgraded"
  fi
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
  [ -n "$http_addr" ] || return 0
  case "$http_addr" in
    127.0.0.1:*|localhost:*|\[::1\]:*) ;;
    *) die "CLIPPORT_HTTP must bind to loopback, got $http_addr" ;;
  esac
}

source_dir() {
  if [ -f "./go.mod" ] && [ -d "./cmd/clipctl" ]; then
    pwd
    return
  fi
  case "$0" in
    */*)
      script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd || true)
      if [ -n "$script_dir" ] && [ -f "$script_dir/go.mod" ] && [ -d "$script_dir/cmd/clipctl" ]; then
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

write_menu_app_bundle() {
  mkdir -p "$app_path/Contents/MacOS" "$app_path/Contents/Resources"
  install -m 0755 "$build_dir/clipport" "$app_path/Contents/MacOS/clipport"
  install -m 0644 "$src/cmd/clipport/assets/app.icns" "$app_path/Contents/Resources/Clipport.icns"
  cat >"$app_path/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>clipport</string>
  <key>CFBundleIdentifier</key>
  <string>com.clipport.app</string>
  <key>CFBundleName</key>
  <string>Clipport</string>
  <key>CFBundleDisplayName</key>
  <string>Clipport</string>
  <key>CFBundleIconFile</key>
  <string>Clipport.icns</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleVersion</key>
  <string>1</string>
  <key>CFBundleShortVersionString</key>
  <string>1.0</string>
  <key>LSUIElement</key>
  <string>1</string>
  <key>NSHighResolutionCapable</key>
  <string>True</string>
</dict>
</plist>
PLIST
  touch "$app_path"
  lsregister="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
  if [ -x "$lsregister" ]; then
    "$lsregister" -f "$app_path" >/dev/null 2>&1 || true
  fi
}

write_app_launch_agent() {
  mkdir -p "$HOME/Library/LaunchAgents"
  cat >"$app_plist_path" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$app_label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$app_path/Contents/MacOS/clipport</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/tmp/clipport.out.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/clipport.err.log</string>
</dict>
</plist>
PLIST
}

[ "$(uname -s)" = "Darwin" ] || die "install.sh currently supports macOS only"
have brew || die "Homebrew is required. Install it from https://brew.sh, then rerun install.sh."

if ! have pngpaste; then
  brew install pngpaste
fi
if ! have go; then
  brew install go
fi

detect_install_action
if app_launch_agent_running || app_process_running; then
  app_was_running=1
fi

src=$(source_dir)
cd "$src"

mkdir -p "$bin_dir"
build_dir=$(mktemp -d)
tmp_dir="${tmp_dir:-}"
trap 'rm -rf "$build_dir"; cleanup' EXIT INT TERM

go build -o "$build_dir/clipctl" ./cmd/clipctl
go build -o "$build_dir/clipportd" ./cmd/clipportd
CGO_ENABLED=1 go build -o "$build_dir/clipport" ./cmd/clipport

install -m 0755 "$build_dir/clipctl" "$bin_dir/clipctl"
install -m 0755 "$build_dir/clipportd" "$bin_dir/clipportd"
install -m 0755 "$build_dir/clipport" "$bin_dir/clipport"

if [ -f "$config_path" ]; then
  config_exists=1
fi
if [ "$config_exists" -eq 0 ] || [ -n "$http_addr" ]; then
  choose_http_addr
fi
validate_http_addr
if [ "$config_exists" -eq 0 ] && [ -z "$iterm_key" ]; then
  iterm_key="0x76-0x120000"
fi
write_menu_app_bundle
write_app_launch_agent
set -- install-record \
  --config "$config_path" \
  --bin-dir "$bin_dir" \
  --app-launchd-plist "$app_plist_path" \
  --app-path "$app_path"
if [ "$config_exists" -eq 0 ]; then
  set -- "$@" --ssh-config "$HOME/.ssh/config"
fi
if [ -n "$http_addr" ]; then
  set -- "$@" --http "$http_addr"
fi
if [ -n "$iterm_key" ]; then
  set -- "$@" --iterm-key "$iterm_key"
fi
"$bin_dir/clipctl" "$@"
if [ "$app_was_running" -eq 1 ]; then
  "$bin_dir/clipctl" restart --config "$config_path" >/dev/null
fi

case ":$PATH:" in
  *":$bin_dir:"*)
    path_hint=""
    clipctl_cmd="clipctl"
    ;;
  *)
    path_hint="export PATH=\"$bin_dir:\$PATH\""
    clipctl_cmd="$bin_dir/clipctl"
    ;;
esac

onboard_cmd="$clipctl_cmd onboard"
default_config_path="$HOME/.config/clipport/config.toml"
if [ "$config_path" != "$default_config_path" ]; then
  onboard_cmd="$onboard_cmd --output $config_path"
fi

if [ -n "$path_hint" ]; then
  say "Before you continue, add Clipport to PATH:"
  say "  $path_hint"
  say
fi

say "Clipport is ${install_result}."
say
if [ "$config_exists" -eq 0 ]; then
  say "Get started:"
  say "  $onboard_cmd"
else
  say "Configure hosts:"
  say "  $clipctl_cmd onboard"
fi
