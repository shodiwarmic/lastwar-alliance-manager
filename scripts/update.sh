#!/bin/bash
# scripts/update.sh — retired. Updates are ./scripts/manage.sh update.
#
# Kept as a signpost for anyone who runs it from habit, permanently: on an install that has
# been migrated to versioned host files it hands over to manage.sh; on one that has not, it
# says the one thing to do. It has no update logic of its own any more, deliberately —
# including the old legacy bare-metal block, which could not tell an old bare-metal install
# from one installed at /opt/lastwar as the guides said, and aborted every update there
# (lastwar-private-docs#137). Self-contained: it sources nothing, because it is the file an
# old install's copy is replaced by.
set -e
cd "$(dirname "$0")/.." || exit 1

if grep -q '^HOST_FILES_VERSION=' .env 2>/dev/null && [ -x scripts/manage.sh ]; then
    echo "Note: update.sh is retired — running ./scripts/manage.sh update for you." >&2
    exec ./scripts/manage.sh update "$@"
fi

echo "This install has not been migrated to versioned host files (v2.0.0)." >&2
echo "Run:  git pull && ./scripts/manage.sh migrate     (see the v2.0.0 release notes)" >&2
exit 1
