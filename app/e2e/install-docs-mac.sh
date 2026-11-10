#!/usr/bin/env bash
# Runs the Mac steps of README.md and INSTALL.md as they are written, on a Mac (CI's Apple silicon and Intel jobs), with
# the release download pointed at a folder holding only this machine's file, built here: a release file can't be
# downloaded before it is released, and a step that picks the wrong file for the Mac finds nothing there.
#
#   app/e2e/install-docs-mac.sh <folder with herdr-anywhere-macos-arm64 or -amd64> <herdr-core>
#
# HM_MACHINE=x86_64 runs the steps under a `uname -m` that says so, to check the Intel Mac's choice of file on an Apple
# silicon Mac; the folder then holds herdr-anywhere-macos-amd64, which must still run here (build it for arm64).
#
# Each document runs in its own fresh home, with herdr-core in ~/.local/bin as Herdr's installer puts it and the
# keys.json Herdr writes when it first opens, so nothing outside a temp folder changes.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
files=$(cd "$1" && pwd)
core=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
download=https://github.com/jt-wang/herdr-anywhere/releases/latest/download/
fail() { echo "FAIL: $*" >&2; exit 1; }

# The first ```sh block of a file that holds $2, without the list's indent.
block() {
  awk -v want="$2" '
    /^ *```sh/ { on = 1; text = ""; next }
    /^ *```/ && on { on = 0; if (index(text, want)) { printf "%s", text; exit } next }
    on { sub(/^   /, ""); text = text $0 "\n" }' "$1"
}

# Runs shell text in a fresh home; prints what it printed, then the home's keys.json.
run() {
  [ -n "$1" ] || fail "empty block"
  case "$1" in *"curl "*"$download"*) ;; *"curl "*) fail "a download that isn't a release file: $1" ;; esac
  local work; work=$(mktemp -d)
  mkdir -p "$work/home/.local/bin" "$work/home/.config/herdr-app" "$work/bin"
  ln -s "$core" "$work/home/.local/bin/herdr-core"
  printf '{\n  "global": {\n    "cmd-t": "tab.new"\n  }\n}\n' > "$work/home/.config/herdr-app/keys.json"
  printf '%s\n' "${1//$download/file://$files/}" > "$work/steps.sh"
  if [ -n "${HM_MACHINE-}" ]; then
    printf '#!/bin/sh\nif [ "$1" = -m ]; then echo %s; else exec /usr/bin/uname "$@"; fi\n' "$HM_MACHINE" > "$work/bin/uname"
    chmod +x "$work/bin/uname"
  fi
  # A shell like an agent's: no hc on PATH, no Go.
  HOME="$work/home" PATH="$work/bin:/usr/bin:/bin:/usr/sbin:/sbin" sh -eu "$work/steps.sh" 2>&1
  echo "keys.json: $(tr -d '\n ' < "$work/home/.config/herdr-app/keys.json")"
}

expect() { grep -qF -- "$2" <<<"$1" || fail "$3 did not print \"$2\":
$1"; }

[ "$(uname -s)" = Darwin ] || fail "this runs the Mac steps, on a Mac"
ls "$files" | grep -qx 'herdr-anywhere-macos-\(amd64\|arm64\)' || fail "$files holds no Mac release file"
[ "$(ls "$files" | wc -l | tr -d ' ')" = 1 ] || fail "$files must hold only this machine's file"

# INSTALL.md 1: herdr-core is found.
out=$(run "$(block "$root/INSTALL.md" 'herdr-core --version')")
expect "$out" "hc " "INSTALL.md step 1"
echo "ok INSTALL.md step 1"

# INSTALL.md 2 and 3: download this machine's file, check the version, then add the dialog.
out=$(run "$(block "$root/INSTALL.md" 'herdr-anywhere-macos-')
$(block "$root/INSTALL.md" 'install-dialog')")
expect "$out" "herdr-anywhere " "INSTALL.md step 2"
expect "$out" "Add phone is installed. Press cmd-shift-m in Herdr" "INSTALL.md step 3"
expect "$out" '"cmd-shift-m":"mod.open:add-phone"' "INSTALL.md step 3's keys.json"
echo "ok INSTALL.md steps 2 and 3 ($(ls "$files"))"

# README's "Or install it yourself", on a Mac.
out=$(run "$(block "$root/README.md" 'herdr-anywhere-macos-')")
expect "$out" "Add phone is installed. Press cmd-shift-m in Herdr" "README.md"
expect "$out" '"cmd-shift-m":"mod.open:add-phone"' "README.md's keys.json"
echo "ok README.md"
echo PASS
