#!/bin/bash
set -euo pipefail
SCRIPT="$(cd "$(dirname "$0")" && pwd -P)/macos-login-agent.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_status() {
  local expected="$1"; shift
  local actual=0
  "$@" >/dev/null 2>&1 || actual=$?
  [[ "$actual" == "$expected" ]] || fail "expected exit $expected, got $actual: $*"
}

bash -n "$SCRIPT" || fail "shell syntax"
[[ "$(bash "$SCRIPT" help | grep -c 'Commands:')" == 1 ]] || fail "help output"
assert_status 1 bash "$SCRIPT" does-not-exist

# Run the status path under a fake uname to validate the unsupported-platform
# guard without touching a real or simulated ~/Library/LaunchAgents.
FAKE_BIN="$(mktemp -d)"
trap 'rm -rf "$FAKE_BIN"' EXIT
printf '#!/bin/sh\nprintf Linux\n' > "$FAKE_BIN/uname"
chmod +x "$FAKE_BIN/uname"
assert_status 1 env PATH="$FAKE_BIN:/usr/bin:/bin" bash "$SCRIPT" status

# Simulate macOS and its utility paths with stubs, so tests do not depend on
# macOS directories or modify the user's login service.
printf '%s\n' '#!/bin/sh' 'printf Darwin' > "$FAKE_BIN/uname"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$FAKE_BIN/lsof"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$FAKE_BIN/plutil"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$FAKE_BIN/PlistBuddy"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$FAKE_BIN/launchctl"
chmod +x "$FAKE_BIN"/*
GO_BIN="$(dirname "$(command -v go)")"
output="$FAKE_BIN/install-output"
status=0
env PATH="$FAKE_BIN:$GO_BIN:/usr/bin:/bin" DENDRITE_TESTING=true DENDRITE_LSOF="$FAKE_BIN/lsof" DENDRITE_PLUTIL="$FAKE_BIN/plutil" DENDRITE_PLISTBUDDY="$FAKE_BIN/PlistBuddy" DENDRITE_LAUNCHCTL="$FAKE_BIN/launchctl" HOME="$FAKE_BIN/home" bash "$SCRIPT" install >"$output" 2>&1 || status=$?
[[ "$status" == 1 ]] || fail "noninteractive install must require explicit confirmation"
grep -q 'confirmation requires an interactive terminal' "$output" || fail "install did not stop at confirmation"
[[ ! -e "$FAKE_BIN/home/Library/LaunchAgents" ]] || fail "noninteractive install created LaunchAgents"

printf '%s\n' 'LaunchAgent script safety tests passed (no service installed).'
