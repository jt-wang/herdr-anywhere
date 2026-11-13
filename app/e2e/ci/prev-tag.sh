#!/bin/sh
# The release before this commit: the newest v* tag that is an ancestor of HEAD and not HEAD itself (on a release tag's
# own run, the one before it). Needs the history and the tags (actions/checkout with fetch-depth: 0).
set -eu
head=$(git rev-parse HEAD)
for t in $(git tag -l 'v*' --sort=-v:refname); do
  c=$(git rev-list -n 1 "$t")
  [ "$c" != "$head" ] && git merge-base --is-ancestor "$c" HEAD && { echo "$t"; exit 0; }
done
echo "no release before this commit" >&2
exit 1
