# shellcheck shell=bash
# tests/scripts/lib/harness.sh — assertions, the stubbed PATH, and helpers shared by every
# *_test.sh. Sourced by run.sh into each test's own process.

# harness_setup — called once per test, before the test file is sourced.
harness_setup() {
    export STUB_DIR="$TEST_TMP/.stubs"
    export STUB_LOG="$STUB_DIR/calls.log"
    export STUB_RESPONSES="$STUB_DIR/responses"
    mkdir -p "$STUB_DIR/bin" "$STUB_RESPONSES"
    : > "$STUB_LOG"
    local name
    for name in sudo crontab sqlite3; do
        ln -s "$TESTS_DIR/lib/stubs/$name" "$STUB_DIR/bin/$name"
    done
    for name in docker systemctl apt-get ufw curl caddy ss getent dpkg openssl fail2ban-client gpg; do
        ln -s "$TESTS_DIR/lib/stubs/stub" "$STUB_DIR/bin/$name"
    done
    export PATH="$STUB_DIR/bin:$PATH"

    # Every system location the scripts write, inside this test's directory.
    export AM_REGISTRY_DIR="$TEST_TMP/sys/etc/alliance-manager/installs.d"
    export AM_BACKUP_DIR="$TEST_TMP/sys/var/backups/lastwar"
    export AM_BACKUP_HELPER="$TEST_TMP/sys/usr/local/bin/backup-lastwar.sh"
    export AM_LOG_DIR="$TEST_TMP/sys/var/log/lastwar"
    export AM_CADDYFILE="$TEST_TMP/sys/etc/caddy/Caddyfile"
    export NON_INTERACTIVE=1
}

# stub_respond NAME <<'SH' ... SH — give stub NAME a behaviour: the body runs with the stub's
# arguments as "$@", and its output and exit status become the stub's.
stub_respond() {
    { echo '#!/bin/bash'; cat; } > "$STUB_RESPONSES/$1"
    chmod +x "$STUB_RESPONSES/$1"
}

# stub_calls NAME — the logged argument lines for NAME, one per call.
stub_calls() {
    sed -n "s/^$1 //p; s/^$1\$//p" "$STUB_LOG"
}

# run CMD... — run CMD in a subshell with `set -e` on, as the scripts run, capturing its
# combined output in $OUT and its exit status in $STATUS. Never fails itself.
run() {
    set +e
    # `exec 2>&1` first, so what an EXIT trap prints on the way out is captured too.
    OUT=$(exec 2>&1; set -eE; "$@")
    STATUS=$?
    set -e
}

_fail() {
    printf 'assertion failed: %s\n' "$*" >&2
    if [ -n "${OUT+x}" ]; then
        printf -- '--- last run output ---\n%s\n-----------------------\n' "$OUT" >&2
    fi
    exit 1
}

assert_eq()        { [ "$1" = "$2" ] || _fail "${3:-values differ}: got [$1], want [$2]"; }
assert_ne()        { [ "$1" != "$2" ] || _fail "${3:-values equal}: both [$1]"; }
assert_status()    { [ "$STATUS" = "$1" ] || _fail "${2:-exit status}: got $STATUS, want $1"; }
assert_contains()  { [[ $1 == *"$2"* ]] || _fail "${3:-missing text}: [$2] not in [$1]"; }
assert_not_contains() { [[ $1 != *"$2"* ]] || _fail "${3:-unexpected text}: [$2] found in [$1]"; }
assert_file()      { [ -f "$1" ] || _fail "${2:-expected file} $1"; }
assert_no_file()   { [ ! -e "$1" ] || _fail "${2:-expected no file} $1"; }
assert_dir()       { [ -d "$1" ] || _fail "${2:-expected directory} $1"; }
assert_no_dir()    { [ ! -e "$1" ] || _fail "${2:-expected no directory} $1"; }
assert_called()    { grep -q "^$1" "$STUB_LOG" || _fail "${2:-expected a call} matching [$1]; calls: $(tr '\n' '|' < "$STUB_LOG")"; }
assert_not_called() { ! grep -q "^$1" "$STUB_LOG" || _fail "${2:-unexpected call} matching [$1]"; }

# line_of PATTERN — the stub-log line number of the first call matching PATTERN (0 if none).
# Used to assert order: "APP_VERSION written before the pull" and the like.
line_of() {
    local n
    n=$(grep -n -m1 "$1" "$STUB_LOG" | cut -d: -f1)
    printf '%s' "${n:-0}"
}

# note_event TEXT — append a marker to the stub log, so a test can order a non-stubbed event
# (a file write) against stubbed calls.
note_event() { printf '# %s\n' "$*" >> "$STUB_LOG"; }
