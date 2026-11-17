#!/bin/sh
set -eu
here=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
xcrun swiftc -module-cache-path "$work/cache" "$here/ios-uitest/tests/PinGestureTrace.swift" "$here/ios-pin-trace-test/main.swift" -o "$work/test"
"$work/test"
