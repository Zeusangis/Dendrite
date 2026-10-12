#!/usr/bin/env bash
# Package the browser extension for Firefox.
#
# Firefox has no background.service_worker, so the Chromium manifest in
# browser-extension/manifest.json cannot be loaded there. This copies the shared
# sources into a directory whose manifest.json is the Firefox one, so the same
# background worker and options page can run in either browser.
#
# Load the packaged directory from about:debugging > This Firefox >
# Load Temporary Add-on (select its manifest.json). For a permanent install,
# sign it at https://addons.mozilla.org with the bundled gecko id.
#
# Usage: package-firefox-extension.sh [output-dir]
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src="$root/browser-extension"
out="${1:-$src/dist-firefox}"

for file in background.js options.html options.js manifest.firefox.json; do
  if [ ! -f "$src/$file" ]; then
    echo "missing $src/$file" >&2
    exit 1
  fi
done

rm -rf "$out"
mkdir -p "$out"
cp "$src/background.js" "$src/options.html" "$src/options.js" "$out/"
cp "$src/manifest.firefox.json" "$out/manifest.json"
echo "Packaged the Firefox extension in $out"
echo "Load it from about:debugging > This Firefox > Load Temporary Add-on."
