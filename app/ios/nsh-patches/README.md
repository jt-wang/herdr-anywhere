# The app's matched NSH runtime

The app remains on NSH v0.3.0 (`76fa8a7fed47ea51aea01fa85bb40960be15d984`),
with patches0001–0009 and Rust1.99.1. `key.sh` hashes the actual patch files;
never write a new key onto an old binary or framework.

0006 backports [`Text shimmer`](https://github.com/jt-wang/nsh/pull/1) from
[`3eee591d`](https://github.com/jt-wang/nsh/commit/3eee591d) without upgrading
across the unrelated changes after v0.3.0. It adds native text paint on iOS
and Android, and a masked inert text copy on the web. The app opts in only
its one existing current Working line. Reduced motion, high contrast and
native low-power guards are implemented by that runtime. Web has no OS
low-power signal; do not claim otherwise.

0007 refreshes iOS keyboard avoidance after a mount batch moves the focused
scroll view in window coordinates. A sheet lifted above the keyboard then
drops its obsolete keyboard inset, keeping its title and input visible.
Unchanged viewports and user scrolling do not re-reveal the input. This is
a native layout check, with no timer, polling or new public API. The focused
iPad regression is `HM_WIDE_FOLDER=1 app/e2e/ios-keyboard.sh`; the default
suite retains the iPhone terminal and folder checks.

0008 does the same for Android: after a mount batch moves the focused input's
scroll view on screen (the app lifts the screen above the keyboard after the
insets came), the scroll view's keyboard padding is computed again from its
new place and the input revealed again. Without it the padding the old place
needed stayed: about 190 dp of blank under the chat's end, and a decision
card's own answer opened cut. Unmoved viewports and user scrolling do not
re-reveal the input. The regression is `app/e2e/android-own-answer.py`
(android-ask.sh runs it).

0009 ends editing when an iOS swipe back has begun, not when it asks to
begin. Resigning there runs before UIKit begins the swipe, as does main-thread
work it brings: in CI's miss, on a terminal with its web editor focused, the
swipe began about half a second late, from wherever the finger had got to,
and UIKit cancelled a pop begun that far along and let go without speed
(about 1 in 288 edge swipes). Begun, the
pop follows the finger from where it started, and the keyboard still hides
during the drag. Holding the main thread 500 ms there, the swipes went back
9 of 24 times before and 24 of 24 after. The regression is
`app/e2e/ios-edge-back.sh` (Devices' ios-edge groups).

## Android and web

Build prerequisites are Rust/rustup, Git, Python3, and a C toolchain. Web
also needs clang and a WebAssembly-capable `llvm-ar` (or set
`AR_wasm32_unknown_unknown`). Android needs `JAVA_HOME` (JDK17 or newer),
`ANDROID_HOME`, SDK35 or newer, build-tools35, and NDK27.2.12479018. The
builder installs the pinned Rust toolchain and its cross-compilation targets.

From the repository root:

```sh
# NSH_HOME, if set, is a read-only override and MUST point to an already matched
# runtime. Unset an override pointing at an installed unpatched distribution.
unset NSH_HOME
app/android/build.sh

# Release package + matching development wasm/JS, for browser diagnostics.
unset NSH_RUNTIME
sh app/scripts/package-web.sh
```

These use an isolated cache under `$XDG_CACHE_HOME/herdr-nsh` (or
`$HOME/.cache/herdr-nsh`), never overwrite the installed NSH distribution,
and select the rebuilt CLI. The first build compiles NSH; later builds
verify the patch key and SHA256 checksums before reusing it. CI's
`nsh-android` action uses the same builder and verifies cache restores.

Web also verifies its build recipe in `nsh-web-build.txt`: the pinned engine
requires an 8 MiB WebAssembly stack for deep UI trees. The builder retains
that setting explicitly alongside remap flags (environment Rust flags
otherwise replace Cargo's target configuration). Do not reuse a web runtime
with a missing or old recipe; use a fresh output, or move the old `web`
subdirectory aside before rebuilding it from the verified source.

For dedicated source/output locations (the source must be fresh and belong
only to this build, not a developer checkout):

```sh
sh app/scripts/nsh-runtime.sh android /build/nsh-source /build/nsh-runtime
sh app/scripts/nsh-runtime.sh web /build/nsh-source /build/nsh-runtime
export NSH_HOME=/build/nsh-runtime NSH_RUNTIME=/build/nsh-runtime
export PATH="$NSH_HOME/cli/bin:$PATH"
```

`--verify-android <out>` and `--verify-web <out>` perform no compilation.
When reusing a separately built CLI, `NSH_CLI_BUILD=<cli output>` must name
one verified by `build-nsh-cli.sh`; the source is also checked against the
exact base+patch stack using a private Git index. Unrelated source edits
and extra source files are rejected, not reset or discarded. Interrupted
initial CLI builds preserve the source; use a fresh dedicated source/output
if that CLI has no successful build marker. A failed runtime build after a
successful CLI can be retried with the same source/output.

The FULL Android AAR contains matching Java classes and all three Rust ABI
libraries. `android/nsh-patches/patch-aar.sh` then applies the existing
pooled-TextLayouts fix, preserving the other classes and native libraries.
That Java-only patcher cannot introduce shimmer into an old release AAR.

## iOS (macOS)

`build-nsh-ios.sh <dedicated source> <embed output>` already applies all
`0*.patch` files; build a new framework with it and use a CLI built by
`build-nsh-cli.sh <another dedicated source> <cli output>`. Set
`NSH_IOS_EMBED` to the framework output and put the matched CLI on `PATH`
before running `app/ios/build.sh`. The existing iOS CI action keys its
framework cache from `key.sh`, so each patch change invalidates the old cache automatically.
The iOS source directory belongs to that builder, which resets/cleans it;
never pass a working checkout. Signing and release approval are unchanged.
