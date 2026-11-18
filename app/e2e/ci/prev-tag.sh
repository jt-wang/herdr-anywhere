#!/bin/sh
# The release before this commit: the newest stable vX.Y.Z tag that is an ancestor of HEAD and not HEAD itself.
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
    git commit -q --allow-empty -m candidate
    git tag v1.0.1
    got=$(sh "$script")
    [ "$got" = v1.0.0 ] || { echo "FAIL: previous stable release: $got" >&2; exit 1; }
  )
  echo 'PASS: previous release skips TestFlight tags and the current commit'
  exit 0
fi
head=$(git rev-parse HEAD)
for t in $(git tag -l 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$'); do
  c=$(git rev-list -n 1 "$t")
  [ "$c" != "$head" ] && git merge-base --is-ancestor "$c" HEAD && { echo "$t"; exit 0; }
done
echo "no release before this commit" >&2
exit 1
