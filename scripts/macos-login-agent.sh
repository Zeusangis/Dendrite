#!/bin/bash
set -euo pipefail

LABEL="com.dendrite.local-server"
BINARY_NAME="dendrite-server"
APP_SUPPORT="$HOME/Library/Application Support/Dendrite"
BINARY="$APP_SUPPORT/bin/$BINARY_NAME"
LOG_DIR="$HOME/Library/Logs/Dendrite"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
PLUTIL_BIN="${DENDRITE_PLUTIL:-/usr/bin/plutil}"
PLISTBUDDY_BIN="${DENDRITE_PLISTBUDDY:-/usr/libexec/PlistBuddy}"
LAUNCHCTL_BIN="${DENDRITE_LAUNCHCTL:-launchctl}"

usage() {
  cat <<'EOF'
Dendrite macOS login service (per-user LaunchAgent; no administrator access)

Usage: scripts/macos-login-agent.sh <command>

Commands:
  install    Build an isolated server and ask before enabling it at login
  status     Show whether it is installed/running and the local API status
  start      Start it for this login; it will also run at future logins
  stop       Stop it for this login; it remains enabled at future logins
  restart    Restart the installed service
  uninstall  Ask before disabling/removing the LaunchAgent (keeps data/logs)
  logs       Show the latest server stdout and error logs
  help       Show this help

Install is always explicit. To stop recording while keeping Dendrite running,
use the Activity dashboard's Stop recording button; `stop` stops the API too.
EOF
}

fail() { printf 'Error: %s\n' "$*" >&2; exit 1; }

require_macos() {
  [[ "$(uname -s)" == "Darwin" ]] || fail "the login service is available only on macOS"
}

confirm() {
  local prompt="$1" answer
  [[ -t 0 ]] || fail "confirmation requires an interactive terminal"
  printf '%s [y/N] ' "$prompt"
  IFS= read -r answer || true
  [[ "$answer" == "y" || "$answer" == "Y" || "$answer" == "yes" || "$answer" == "YES" ]]
}

launch_domain() { printf 'gui/%s' "$(id -u)"; }

is_loaded() { "$LAUNCHCTL_BIN" print "$(launch_domain)/$LABEL" >/dev/null 2>&1; }

bootstrap_service() {
  [[ -f "$PLIST" ]] || fail "service is not installed; run install first"
  "$LAUNCHCTL_BIN" enable "$(launch_domain)/$LABEL"
  if is_loaded; then
    "$LAUNCHCTL_BIN" kickstart -k "$(launch_domain)/$LABEL"
  else
    "$LAUNCHCTL_BIN" bootstrap "$(launch_domain)" "$PLIST"
  fi
}

plist_string() {
  local key="$1" value="$2"
  # PlistBuddy accepts a quoted string value. Escape backslashes and quotes
  # before placing a filesystem path in its command language.
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  "$PLISTBUDDY_BIN" -c "Add $key string \"$value\"" "$tmp_plist"
}

plist_bool() { "$PLISTBUDDY_BIN" -c "Add $1 bool $2" "$tmp_plist"; }
plist_integer() { "$PLISTBUDDY_BIN" -c "Add $1 integer $2" "$tmp_plist"; }

rollback_install() {
  local reason="$1" restore_error=false
  # Remove only a job loaded from the new plist. An unrelated API instance is
  # never stopped or signalled as part of rolling an installation back.
  if is_loaded; then "$LAUNCHCTL_BIN" bootout "$(launch_domain)/$LABEL" >/dev/null 2>&1 || true; fi
  if [[ "$had_plist" == true ]]; then mv -f "$old_plist" "$PLIST" || restore_error=true; else rm -f "$PLIST"; fi
  if [[ "$had_binary" == true ]]; then mv -f "$old_binary" "$BINARY" || restore_error=true; else rm -f "$BINARY"; fi
  if [[ "$had_plist" == true ]]; then
    "$LAUNCHCTL_BIN" enable "$(launch_domain)/$LABEL" || restore_error=true
    if [[ "$was_loaded" == true ]]; then
      if is_loaded; then "$LAUNCHCTL_BIN" kickstart -k "$(launch_domain)/$LABEL" || restore_error=true
      else "$LAUNCHCTL_BIN" bootstrap "$(launch_domain)" "$PLIST" || restore_error=true; fi
    fi
  elif [[ "${enable_attempted:-false}" == true ]]; then
    "$LAUNCHCTL_BIN" disable "$(launch_domain)/$LABEL" || restore_error=true
  fi
  if [[ "$restore_error" == true ]]; then fail "$reason; automatic rollback was incomplete, inspect $PLIST and launchctl"; fi
  fail "$reason; the previous service configuration was restored"
}

install_service() {
  require_macos
  command -v go >/dev/null 2>&1 || fail "Go is required to build the background server"
  [[ -d "$REPO_ROOT/backend" ]] || fail "backend directory not found from $REPO_ROOT"
  local lsof_bin="${DENDRITE_LSOF:-/usr/sbin/lsof}"
  if [[ -f "$PLIST" ]] && ! is_loaded; then
    fail "LaunchAgent is installed but not loaded; run start or uninstall before reinstalling"
  fi
  local prompt="Install Dendrite to start at your macOS user login and run in the background?"
  [[ -f "$PLIST" ]] && prompt="Update the installed Dendrite login service and server binary?"
  if ! confirm "$prompt"; then
    printf 'Install cancelled; no service or files were changed.\n'
    return 0
  fi
  if [[ -x "$lsof_bin" ]] && ! is_loaded && "$lsof_bin" -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1; then
    fail "127.0.0.1:8080 is already in use; stop the existing server before installing the login service"
  fi

  umask 077
  mkdir -p "$APP_SUPPORT/bin" "$LOG_DIR" "$HOME/Library/LaunchAgents"
  chmod 700 "$APP_SUPPORT" "$APP_SUPPORT/bin" "$LOG_DIR"
  local tmp_binary="$BINARY.tmp.$$" tmp_plist="$PLIST.tmp.$$"
  local old_binary="$BINARY.previous.$$" old_plist="$PLIST.previous.$$"
  local had_binary=false had_plist=false was_loaded=false enable_attempted=false
  if [[ -f "$BINARY" ]]; then cp -p "$BINARY" "$old_binary"; had_binary=true; fi
  if [[ -f "$PLIST" ]]; then cp -p "$PLIST" "$old_plist"; had_plist=true; fi
  if is_loaded; then was_loaded=true; fi
  # Keep rollback copies on unexpected interruption/failure. They are removed
  # only after launchd confirms the replacement is loaded successfully.
  trap 'rm -f "$tmp_binary" "$tmp_plist"' EXIT

  printf 'Building Dendrite server for macOS…\n'
  (cd "$REPO_ROOT/backend" && go build -o "$tmp_binary" ./cmd/server)
  chmod 700 "$tmp_binary"

  "$PLUTIL_BIN" -create xml1 "$tmp_plist"
  plist_string ':Label' "$LABEL"
  "$PLISTBUDDY_BIN" -c 'Add :ProgramArguments array' "$tmp_plist"
  plist_string ':ProgramArguments:0' "$BINARY"
  "$PLISTBUDDY_BIN" -c 'Add :EnvironmentVariables dict' "$tmp_plist"
  plist_string ':EnvironmentVariables:DENDRITE_ROOT' "$REPO_ROOT"
  if [[ "${DENDRITE_ACTIVITY_AUTOSTART:-}" == "false" ]]; then
    plist_string ':EnvironmentVariables:DENDRITE_ACTIVITY_AUTOSTART' 'false'
  fi
  plist_string ':WorkingDirectory' "$REPO_ROOT"
  plist_bool ':RunAtLoad' true
  plist_bool ':KeepAlive' true
  plist_integer ':ThrottleInterval' 10
  plist_string ':LimitLoadToSessionType' 'Aqua'
  plist_string ':StandardOutPath' "$LOG_DIR/server.log"
  plist_string ':StandardErrorPath' "$LOG_DIR/server-error.log"

  # Validate before replacing the registered configuration. Existing service
  # remains untouched until the new binary/plist are built and validated.
  "$PLUTIL_BIN" -lint "$tmp_plist"
  if [[ "$was_loaded" == true ]] && ! "$LAUNCHCTL_BIN" bootout "$(launch_domain)/$LABEL"; then
    if is_loaded; then fail "could not stop the existing service; its files were left untouched"; fi
    rollback_install "could not stop the existing service"
  fi
  if ! mv -f "$tmp_binary" "$BINARY"; then rollback_install "could not install the new server binary"; fi
  if ! mv -f "$tmp_plist" "$PLIST"; then rollback_install "could not install the LaunchAgent configuration"; fi
  enable_attempted=true
  if ! "$LAUNCHCTL_BIN" enable "$(launch_domain)/$LABEL"; then rollback_install "could not enable LaunchAgent"; fi
  # A prior loaded service is booted out before the binary/plist swap, so a
  # bootstrap (not kickstart) is required for both installs and updates.
  if ! "$LAUNCHCTL_BIN" bootstrap "$(launch_domain)" "$PLIST"; then
    rollback_install "could not start LaunchAgent"
  fi
  # Ensure the replacement actually registered before discarding rollback data.
  if ! is_loaded; then rollback_install "LaunchAgent bootstrap returned without loading the service"; fi
  trap - EXIT
  rm -f "$old_binary" "$old_plist"

  printf '\nDendrite is installed as a per-user login service.\n'
  printf 'Checkout: %s\nBinary:   %s\nLogs:     %s\n' "$REPO_ROOT" "$BINARY" "$LOG_DIR"
  printf 'The service uses %s/notes and %s/data.\n' "$REPO_ROOT" "$REPO_ROOT"
  if [[ "${DENDRITE_ACTIVITY_AUTOSTART:-}" == "false" ]]; then
    printf 'Activity collection is disabled by DENDRITE_ACTIVITY_AUTOSTART=false.\n'
  else
    printf 'Activity collection is enabled by default; Stop it in the Activity dashboard when desired.\n'
  fi
  printf 'To grant window-title access, add the Dendrite binary at:\n  %s\n' "$BINARY"
  printf 'System Settings → Privacy & Security → Accessibility.\n'
  printf 'Manage the service with this script; uninstalling preserves your notes, database, and logs.\n'
}

status_service() {
  require_macos
  if [[ ! -f "$PLIST" ]]; then
    printf 'Login service: not installed\n'
  elif is_loaded; then
    printf 'Login service: installed and loaded\n'
  else
    printf 'Login service: installed but not loaded for this login\n'
  fi
  if [[ -x "$BINARY" ]]; then printf 'Server binary: %s\n' "$BINARY"; fi
  if "${DENDRITE_CURL:-/usr/bin/curl}" --fail --silent --max-time 2 http://127.0.0.1:8080/api/health >/dev/null 2>&1; then
    printf 'Local API:     running\n'
    printf 'Activity:      '
    "${DENDRITE_CURL:-/usr/bin/curl}" --fail --silent --max-time 2 http://127.0.0.1:8080/api/activity || true
    printf '\n'
  else
    printf 'Local API:     not responding on 127.0.0.1:8080\n'
  fi
}

start_service() {
  require_macos
  bootstrap_service
  printf 'Dendrite service started for this login and remains enabled at future logins.\n'
}

stop_service() {
  require_macos
  if is_loaded; then
    "$LAUNCHCTL_BIN" bootout "$(launch_domain)/$LABEL"
    printf 'Dendrite stopped for this login. It will start again at the next login.\n'
  else
    printf 'Dendrite is not running for this login.\n'
  fi
}

uninstall_service() {
  require_macos
  if [[ ! -f "$PLIST" ]]; then printf 'Login service is not installed.\n'; return 0; fi
  if ! confirm 'Disable and remove the Dendrite login service? Notes, database, binary, and logs will be kept.'; then
    printf 'Uninstall cancelled; no files were changed.\n'
    return 0
  fi
  if is_loaded; then "$LAUNCHCTL_BIN" bootout "$(launch_domain)/$LABEL"; fi
  "$LAUNCHCTL_BIN" disable "$(launch_domain)/$LABEL"
  rm -f "$PLIST"
  printf 'Login service removed. Notes, database, binary, and logs were preserved.\n'
}

show_logs() {
  require_macos
  local found=0 file
  for file in "$LOG_DIR/server.log" "$LOG_DIR/server-error.log"; do
    if [[ -f "$file" ]]; then
      found=1
      printf '\n--- %s ---\n' "$file"
      tail -n 80 "$file"
    fi
  done
  (( found == 1 )) || printf 'No LaunchAgent logs yet (%s).\n' "$LOG_DIR"
}

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
command_name="${1:-help}"
case "$command_name" in
  install) install_service ;;
  status) status_service ;;
  start) start_service ;;
  stop) stop_service ;;
  restart) stop_service; start_service ;;
  uninstall) uninstall_service ;;
  logs) show_logs ;;
  help|-h|--help) usage ;;
  *) usage >&2; fail "unknown command: $command_name" ;;
esac
