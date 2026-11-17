#!/bin/sh
# Reproduce the reactive and QuickJS lifetime bugs with the upstream tests, then test the patched crates.
# Run in an isolated Linux build container with Rust, clang/libclang and cmake installed.
# Usage: test-nsh.sh <nsh git clone> [log directory]
# The clone is read-only here; builds use a temporary export. CARGO_TARGET_DIR may hold a build cache.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
src=${1:?nsh clone}
out=${2:-$(mktemp -d)}
mkdir -p "$out"
out=$(cd "$out" && pwd)
base=76fa8a7fed47ea51aea01fa85bb40960be15d984
[ "$(git -C "$src" rev-parse 'v0.3.0^{commit}')" = "$base" ] || { echo "unexpected v0.3.0" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
toolchain=$("$here/key.sh" | awk '{print $4}')
git -C "$src" archive "$base" > "$work/base.tar"
mkdir "$work/source" "$work/red" "$work/green"
tar -xf "$work/base.tar" -C "$work/source"
cp "$work/source/crates/nsh-core/src/reactive.rs" "$work/red/mod.rs"
for p in "$here"/0*.patch; do (cd "$work/source" && git apply "$p"); done
cp "$work/source/crates/nsh-core/src/reactive.rs" "$work/green/mod.rs"
cp "$work/source/crates/nsh-core/src/reactive/tests.rs" "$work/red/tests.rs"
cp "$work/red/tests.rs" "$work/green/tests.rs"

# reactive.rs uses only std. Copying it as mod.rs preserves its tests module without changing its code.
rustup run "$toolchain" rustc --edition=2024 --test "$work/red/mod.rs" -o "$work/red/tests"
if "$work/red/tests" > "$out/core-red.log" 2>&1; then
  echo "FAIL: the original graph passed the regression tests" >&2; exit 1
fi
for name in a_computation_disposed_in_a_nested_run_tracks_nothing_after \
  a_computation_disposed_while_it_runs_is_left_with_no_edges \
  a_memo_read_while_it_is_disposed_does_not_run_again \
  an_owner_disposed_again_by_its_cleanups_is_freed_once \
  a_root_whose_owner_went_does_not_loop_through_its_reused_slot; do
  grep -q "test tests::$name .*FAILED" "$out/core-red.log" || { echo "FAIL: no expected failure for $name" >&2; exit 1; }
done
rustup run "$toolchain" rustc --edition=2024 --test "$work/green/mod.rs" -o "$work/green/tests"
"$work/green/tests" > "$out/core-green.log" 2>&1

# This test also passes on the old graph; prove it catches freeing the handle during dispose in the new graph.
perl -0pi -e 's/\(n\.disposed && !n\.disposing\)/n.disposed/ or die "missing disposing rule"' "$work/green/mod.rs"
rustup run "$toolchain" rustc --edition=2024 --test "$work/green/mod.rs" -o "$work/green/mutant"
if "$work/green/mutant" a_memo_whose_handle_goes_while_it_is_disposed_is_freed_once > "$out/core-handle-red.log" 2>&1; then
  echo "FAIL: freeing the handle during dispose escaped detection" >&2; exit 1
fi
grep -q 'test tests::a_memo_whose_handle_goes_while_it_is_disposed_is_freed_once .*FAILED' "$out/core-handle-red.log"

export CARGO_BUILD_JOBS=${CARGO_BUILD_JOBS:-1}
export CARGO_PROFILE_DEV_DEBUG=0 CARGO_PROFILE_TEST_DEBUG=0 CARGO_INCREMENTAL=0
js="$work/source/crates/nsh-js/src/reactive.rs"
cp "$js" "$work/js-fixed.rs"
perl -0pi -e 's/ && g\.held_by_handle\(id\)// or die "missing memo ownership rule"' "$js"
cd "$work/source"
if rustup run "$toolchain" cargo test --locked -p nsh-js --lib a_live_memo_keeps_its_value_when_its_object_is_collected -- --nocapture > "$out/js-red.log" 2>&1; then
  echo "FAIL: the old GC ownership rule passed" >&2; exit 1
fi
# A compile error or resource kill is not the expected red result.
grep -q "the live memo's value was collected" "$out/js-red.log"
cp "$work/js-fixed.rs" "$js"
# Keep the interrupt regression while reverting only its QuickJS dependency fix. An interrupted find must fail
# with the reference-count assertion on 0.14.1, even with all three reactive fixes already applied.
git apply -R --exclude=crates/nsh-js/src/host.rs "$here/0005-nsh-835-interrupted-find.patch"
if rustup run "$toolchain" cargo test --locked -p nsh-js --lib an_interrupt_inside_find -- --nocapture > "$out/interrupt-red.log" 2>&1; then
  echo "FAIL: QuickJS 0.14.1 passed the interrupted find regression" >&2; exit 1
fi
grep -q 'gc_decref_child: Assertion' "$out/interrupt-red.log"
grep -Fq 'JS_REF_COUNT(p) > 0' "$out/interrupt-red.log"
git apply --exclude=crates/nsh-js/src/host.rs "$here/0005-nsh-835-interrupted-find.patch"
rustup run "$toolchain" cargo test --locked -p nsh-core -p nsh-js --lib > "$out/crates-green.log" 2>&1
grep 'test result:' "$out/core-green.log" "$out/crates-green.log"
echo "PASS: all four runtime fixes have failing controls and passing regressions"
