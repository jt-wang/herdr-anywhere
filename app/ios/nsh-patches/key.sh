#!/bin/sh
# What NativeShell is built from: nsh v0.3.0, the toolchain and the patches' sha256. build-nsh-ios.sh writes it
# beside the framework, ios/build.sh checks it, and CI caches the framework under it.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
printf 'nsh v0.3.0 rust 1.99.1 patches %s\n' "$(cat "$here"/0*.patch | shasum -a 256 | cut -c1-16)"
