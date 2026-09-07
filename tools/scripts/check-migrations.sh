#!/bin/sh
# CI passes a verified base commit; changes to existing migrations are forbidden.
set -eu
base_commit=$1
git rev-parse --verify "${base_commit}^{commit}" >/dev/null
changes=$(git diff --name-status --no-renames --diff-filter=MD "${base_commit}" HEAD -- migrations/)
if [ -n "${changes}" ]; then
  printf '%s\n' "Published/base migrations must not be modified or deleted; add an incremental migration:" "${changes}" >&2
  exit 1
fi
