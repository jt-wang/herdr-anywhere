#!/bin/sh
# Run SpaceOpener's behavior tests on macOS, without a simulator or app build.
# Usage: sh app/e2e/ios-open-space-test.sh [--mutations | OpenSpace.swift]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
if [ "${1:-}" = --mutations ]; then exec python3 "$here/ios-open-space-test/mutations.py"; fi
source=${1:-"$here/ios-uitest/tests/OpenSpace.swift"}
work=$(mktemp -d)
trap 'rm -rf "${work:?}"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Only XCTest's UI doubles change; the SpaceOpener implementation is compiled as written.
sed 's/^import XCTest$/import Foundation\
import CoreGraphics/' "$source" > "$work/OpenSpace.swift"
xcrun swiftc -module-cache-path "$work/cache" "$work/OpenSpace.swift" "$here/ios-open-space-test/main.swift" -o "$work/test"
"$work/test" "$work"
