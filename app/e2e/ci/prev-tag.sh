#!/bin/sh
# The release before this app version: the newest stable ancestor tag, excluding HEAD and its app version.
# A tag for this version can exist before its assets are published, including on later maintenance commits.
# TestFlight-only tags have no downloadable release assets. Needs history and tags (checkout with fetch-depth: 0).
set -eu
if [ "${1:-}" = --self-test ]; then
  script=$(realpath "$0")
  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  (
    cd "$work"
    git init -q
    git config user.name 'Release Fixture'
    git config user.email 'release@example.org'
    git config commit.gpgsign false
    git commit -q --allow-empty -m stable
    git tag v1.0.0
    git commit -q --allow-empty -m testflight
    git tag v1.0.1-testflight.1
    mkdir app
    printf '{"version":"1.0.1"}\n' > app/nsh.json
    git add app/nsh.json
    git commit -q -m candidate
    git tag v1.0.1
    got=$(sh "$script")
    [ "$got" = v1.0.0 ] || { echo "FAIL: previous stable release: $got" >&2; exit 1; }
    git commit -q --allow-empty -m maintenance
    got=$(sh "$script")
    [ "$got" = v1.0.0 ] || { echo "FAIL: current version after its tag: $got" >&2; exit 1; }
  )
  echo 'PASS: previous release skips TestFlight tags and the current version, including after its tag'
  exit 0
fi
head=$(git rev-parse HEAD)
version=$(git show HEAD:app/nsh.json | python3 -c 'import json, sys; print("v" + json.load(sys.stdin)["version"])')
for t in $(git tag -l 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$'); do
  [ "$t" != "$version" ] || continue
  c=$(git rev-list -n 1 "$t")
  [ "$c" != "$head" ] && git merge-base --is-ancestor "$c" HEAD && { echo "$t"; exit 0; }
done
echo "no release before this commit" >&2
exit 1
