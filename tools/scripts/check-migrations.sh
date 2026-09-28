#!/bin/sh
# CI passes a released commit; published migrations must not be changed.
set -eu
base_commit=$1
git rev-parse --verify "${base_commit}^{commit}" >/dev/null
changes=$(git diff --name-status --no-renames --diff-filter=MD "${base_commit}" -- migrations/)
if [ -n "${changes}" ]; then
  printf '%s\n' "Published migrations must not be modified or deleted; add an incremental migration:" "${changes}" >&2
  exit 1
fi
