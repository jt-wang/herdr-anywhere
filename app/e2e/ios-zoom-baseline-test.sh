#!/bin/sh
# Execute the actual baseline guard with scripted system alert outcomes, without a simulator.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "${work:?}"' EXIT
sed 's/^import XCTest$/import Foundation/' "${1:-$here/ios-uitest/tests/ZoomBaseline.swift}" > "$work/ZoomBaseline.swift"
xcrun swiftc -module-cache-path "$work/cache" "$work/ZoomBaseline.swift" "$here/ios-zoom-baseline-test/main.swift" -o "$work/test"
"$work/test"
