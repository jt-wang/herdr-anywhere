#!/usr/bin/env bash
# Runs the PowerShell steps of README.md and INSTALL.md as they are written, on Windows (CI's Windows jobs), with the
# release download swapped for a companion built here: a release file can't be downloaded before it is released.
#
#   app/e2e/install-docs-windows.sh <herdr-anywhere-windows-amd64.exe> <herdr-core.exe>
#
# Each document runs in its own fresh %LOCALAPPDATA% and Herdr home (HERDR_APP_HOME, with a keys.json like Herdr's),
# so nothing outside a temp folder changes.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
exe=$(cygpath -m "$1")
core_dir=$(dirname "$(cygpath -u "$2")")
url=https://github.com/jt-wang/herdr-anywhere/releases/latest/download/herdr-anywhere-windows-amd64.exe
fail() { echo "FAIL: $*" >&2; exit 1; }

# The n-th ```powershell block of a file (1-based), without the list's indent.
block() {
  awk -v n="$2" '/^ *```powershell/ { i++; on = (i == n); next } /^ *```/ { on = 0; next } on { sub(/^   /, ""); print }' "$1"
}

# Runs PowerShell text in a fresh %LOCALAPPDATA% and Herdr home; prints what it printed.
run() {
  local work; work=$(mktemp -d)
  mkdir -p "$work/local" "$work/app"
  printf '{\n  "global": {\n    "ctrl-shift-t": "tab.new"\n  }\n}\n' > "$work/app/keys.json"
  [ -n "$1" ] || fail "empty block"
  case "$1" in *"$url"*) ;; *"curl.exe"*) fail "a download that isn't the release file: $1" ;; esac
  printf '%s\n' "${1//$url/file:///$exe}" > "$work/steps.ps1"
  LOCALAPPDATA=$(cygpath -w "$work/local") HERDR_APP_HOME=$(cygpath -w "$work/app") PATH="$PATH:$core_dir" \
    pwsh -NoProfile -NonInteractive -File "$(cygpath -w "$work/steps.ps1")" 2>&1
  echo "keys.json: $(tr -d '\n ' < "$work/app/keys.json")"
}

expect() { grep -qF -- "$2" <<<"$1" || fail "$3 did not print \"$2\":
$1"; }

# INSTALL.md 1: herdr-core is found ("hc" isn't on PATH here, herdr-core is).
out=$(run "$(block "$root/INSTALL.md" 1)")
expect "$out" "hc " "INSTALL.md step 1"
echo "ok INSTALL.md step 1"

# INSTALL.md 2 and 3: download, check the version, then add the dialog by the full path step 2 names.
steps=$(block "$root/INSTALL.md" 2)
path=$(grep -o '& "[^"]*herdr-anywhere.exe"' <<<"$steps" | head -1)
[ -n "$path" ] || fail "INSTALL.md step 2 runs no herdr-anywhere.exe"
grep -qF "\`$path\`" "$root/INSTALL.md" || fail "INSTALL.md doesn't tell the agent to call it by $path"
out=$(run "$steps
$path install-dialog")
expect "$out" "herdr-anywhere " "INSTALL.md step 2"
expect "$out" "Add phone is installed. Press ctrl-shift-m in Herdr" "INSTALL.md step 3"
expect "$out" '"ctrl-shift-m":"mod.open:add-phone"' "INSTALL.md step 3's keys.json"
echo "ok INSTALL.md steps 2 and 3"

# README's "Or install it yourself", on Windows.
out=$(run "$(block "$root/README.md" 1)")
expect "$out" "Add phone is installed. Press ctrl-shift-m in Herdr" "README.md"
expect "$out" '"ctrl-shift-m":"mod.open:add-phone"' "README.md's keys.json"
echo "ok README.md"
echo PASS
