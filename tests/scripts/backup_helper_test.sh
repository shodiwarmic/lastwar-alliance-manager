# shellcheck shell=bash
# Tests for the nightly backup helper (backup_helper_text / ensure_backup_helper in
# scripts/lib/common.sh) — the one routine root's cron runs.
# shellcheck source=../../scripts/lib/common.sh
source "$REPO_ROOT/scripts/lib/common.sh"
# shellcheck source=../../scripts/lib/registry.sh
source "$REPO_ROOT/scripts/lib/registry.sh"

# An install with a database, and a scripts/manage.sh that leaves a mark if anything runs it.
make_install() {
    mkdir -p "$1/data" "$1/scripts"
    touch "$1/docker-compose.yml"
    echo "db of $1" > "$1/data/alliance.db"
    printf '#!/bin/bash\ntouch "%s/RAN"\n' "$TEST_TMP" > "$1/scripts/manage.sh"
    chmod +x "$1/scripts/manage.sh"
}

write_helper() {
    mkdir -p "$(dirname "$AM_BACKUP_HELPER")"
    backup_helper_text "$AM_REGISTRY_DIR" "$AM_BACKUP_DIR" > "$AM_BACKUP_HELPER"
    chmod +x "$AM_BACKUP_HELPER"
}

test_backs_up_every_registered_install_under_its_name() {
    make_install "$TEST_TMP/a"
    make_install "$TEST_TMP/b"
    registry_write default "$TEST_TMP/a"
    registry_write second "$TEST_TMP/b"
    write_helper
    : > "$STUB_LOG"   # only the helper's own calls from here
    run "$AM_BACKUP_HELPER"
    assert_status 0
    compgen -G "$AM_BACKUP_DIR/nightly_default_*.db" >/dev/null || _fail "no nightly_default_ backup"
    compgen -G "$AM_BACKUP_DIR/nightly_second_*.db" >/dev/null || _fail "no nightly_second_ backup"
    assert_eq "$(cat "$AM_BACKUP_DIR"/nightly_second_*.db)" "db of $TEST_TMP/b"
    assert_no_file "$TEST_TMP/RAN" "the helper executed a file from an install directory"
    # sqlite3 is the only stubbed command it runs (tolerating a database with no -wal/-shm).
    assert_eq "$(grep -v '^sqlite3 ' "$STUB_LOG" | grep -vc '^#' || true)" 0
}

test_prunes_only_old_nightlies() {
    make_install "$TEST_TMP/a"
    registry_write default "$TEST_TMP/a"
    mkdir -p "$AM_BACKUP_DIR"
    touch -d '10 days ago' "$AM_BACKUP_DIR/nightly_default_old.db" "$AM_BACKUP_DIR/alliance_legacy.db" \
        "$AM_BACKUP_DIR/db_20200101_000000.db"
    touch -d '2 days ago' "$AM_BACKUP_DIR/nightly_default_recent.db"
    write_helper
    run "$AM_BACKUP_HELPER"
    assert_status 0
    assert_no_file "$AM_BACKUP_DIR/nightly_default_old.db"
    assert_no_file "$AM_BACKUP_DIR/alliance_legacy.db" "the pre-v2.0.0 nightly pool keeps its 7-day rule"
    assert_file "$AM_BACKUP_DIR/nightly_default_recent.db"
    assert_file "$AM_BACKUP_DIR/db_20200101_000000.db" "the pre-update pool is not the helper's to prune"
}

test_hands_wal_and_shm_back_to_the_database_owner() {
    make_install "$TEST_TMP/a"
    touch "$TEST_TMP/a/data/alliance.db-wal"
    registry_write default "$TEST_TMP/a"
    write_helper
    run "$AM_BACKUP_HELPER"
    assert_status 0
}

test_empty_registry_fails_with_a_message() {
    write_helper
    run "$AM_BACKUP_HELPER"
    assert_status 1
    assert_contains "$OUT" "no registered installs"
}

test_ensure_writes_once_and_adds_one_crontab_line() {
    run ensure_backup_helper
    assert_status 0
    assert_contains "$OUT" "written"
    assert_file "$AM_BACKUP_HELPER"
    run ensure_backup_helper
    assert_contains "$OUT" "unchanged"
    assert_eq "$(grep -c "$AM_BACKUP_HELPER" "$STUB_DIR/crontab")" 1
    assert_contains "$(cat "$STUB_DIR/crontab")" "0 2 * * * $AM_BACKUP_HELPER >> $AM_LOG_DIR/backup.log 2>&1"
}

test_ensure_replaces_the_old_shape_and_keeps_a_foreign_file() {
    mkdir -p "$(dirname "$AM_BACKUP_HELPER")"
    printf '#!/bin/bash\nBACKUP_DIR="/var/backups/lastwar"\nDB_PATH="/opt/lastwar/data/alliance.db"\n' > "$AM_BACKUP_HELPER"
    run ensure_backup_helper
    assert_contains "$OUT" "replaced"
    assert_not_contains "$OUT" "not ours"
    printf '#!/bin/bash\necho somebody else\n' > "$AM_BACKUP_HELPER"
    run ensure_backup_helper
    assert_contains "$OUT" "not ours"
    compgen -G "$AM_BACKUP_HELPER.backup_*" >/dev/null || _fail "the foreign helper was not kept"
    grep -q 'every registered Alliance Manager install' "$AM_BACKUP_HELPER" || _fail "helper not replaced"
}
