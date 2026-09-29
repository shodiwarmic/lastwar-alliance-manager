#!/bin/bash
# tests/scripts/run.sh — test runner for the host scripts (scripts/install.sh, scripts/manage.sh,
# scripts/update.sh and scripts/lib/). Plain bash, deliberately not bats: one less thing to
# install on a runner or a dev box, and the whole harness fits in lib/harness.sh.
#
#   bash tests/scripts/run.sh               # every *_test.sh
#   bash tests/scripts/run.sh manage        # files whose name contains "manage"
#   bash tests/scripts/run.sh manage prune  # ...and only tests whose name contains "prune"
#
# Each test_* function runs in its own bash process, in its own temp directory, with the
# stubs in lib/stubs first on PATH and every system path (registry, backups, Caddyfile,
# backup helper) pointed into that directory. Nothing a test does can reach the real host.
set -u
TESTS_DIR=$(cd "$(dirname "$0")" && pwd)
REPO_ROOT=$(cd "$TESTS_DIR/../.." && pwd)
export TESTS_DIR REPO_ROOT
file_filter=${1:-}
test_filter=${2:-}

pass=0
fail=0
failed=()
for file in "$TESTS_DIR"/*_test.sh; do
    [ -e "$file" ] || continue
    case $(basename "$file") in *"$file_filter"*) ;; *) continue ;; esac
    # Listing a file's tests sources it, so it gets the same sandbox a test does.
    tmp=$(mktemp -d)
    tests=$(cd "$tmp" && TEST_TMP="$tmp" \
        bash -c 'source "$1"; harness_setup; source "$2"; declare -F' _ "$TESTS_DIR/lib/harness.sh" "$file" \
        | awk '$3 ~ /^test_/ {print $3}')
    rm -rf "$tmp"
    for t in $tests; do
        case $t in *"$test_filter"*) ;; *) continue ;; esac
        tmp=$(mktemp -d)
        out=$(cd "$tmp" && TEST_TMP="$tmp" \
            bash -c 'source "$TESTS_DIR/lib/harness.sh"; harness_setup; source "$1"; set -eE; "$2"' \
            _ "$file" "$t" 2>&1)
        status=$?
        if [ $status -eq 0 ]; then
            pass=$((pass + 1))
            printf 'PASS %s: %s\n' "$(basename "$file" .sh)" "$t"
        else
            fail=$((fail + 1))
            failed+=("$(basename "$file" .sh): $t")
            printf 'FAIL %s: %s (exit %d)\n' "$(basename "$file" .sh)" "$t" "$status"
            printf '%s\n' "$out" | sed 's/^/    | /'
        fi
        rm -rf "$tmp"
    done
done

printf '\n%d passed, %d failed\n' "$pass" "$fail"
if [ $((pass + fail)) -eq 0 ]; then
    echo "No tests ran — check the filter, or whether tests/scripts moved." >&2
    exit 1
fi
if [ $fail -gt 0 ]; then
    printf '  %s\n' "${failed[@]}"
    exit 1
fi
