#!/bin/bash
# Deprecated shim — the real script moved to scripts/update.sh.
#
# This exists for exactly one release. An install that predates the move runs the OLD
# root update.sh, which does a `git pull` and then hands control back; without this file
# the operator would be left with nothing to run next time. Removal is tracked by
# shodiwarmic/lastwar-private-docs#71.
set -e
echo "Note: update.sh has moved to scripts/update.sh — running it for you." >&2
echo "      Use ./scripts/update.sh from now on." >&2
echo "" >&2
exec "$(dirname "$0")/scripts/update.sh" "$@"
