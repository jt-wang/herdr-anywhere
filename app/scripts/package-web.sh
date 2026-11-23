#!/bin/sh
# Package with the same patched runtime as the CLI, not v0.3.0's old wasm.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd -P)
runtime=${NSH_RUNTIME:-${XDG_CACHE_HOME:-$HOME/.cache}/herdr-nsh/$("$root/ios/nsh-patches/key.sh" | tr ' ' '-')/runtime}
if [ -n "${NSH_RUNTIME:-}" ]; then
  sh "$root/scripts/nsh-runtime.sh" --verify-web "$runtime"
else
  sh "$root/scripts/nsh-runtime.sh" web "$(dirname "$runtime")/source" "$runtime"
fi
export NSH_RUNTIME="$runtime" PATH="$runtime/cli/bin:$PATH"
cd "$root"
exec nsh package --target web -o "${1:-dist/web}"
