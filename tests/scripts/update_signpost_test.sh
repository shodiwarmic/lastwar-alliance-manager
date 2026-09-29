# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for scripts/update.sh — the retired updater, now a signpost.

# An install with the signpost and a manage.sh that records how it was called.
make_install() {
    APP="$TEST_TMP/app"
    mkdir -p "$APP/scripts"
    cp "$REPO_ROOT/scripts/update.sh" "$APP/scripts/update.sh"
    printf '#!/bin/bash\nprintf "%%s\\n" "$@" > "%s/manage-args"\n' "$TEST_TMP" > "$APP/scripts/manage.sh"
    chmod +x "$APP/scripts/update.sh" "$APP/scripts/manage.sh"
    printf 'APP_VERSION=v2.0.0\n' > "$APP/.env"
}

test_a_migrated_install_is_handed_to_manage() {
    make_install
    echo HOST_FILES_VERSION=v2.0.0 >> "$APP/.env"
    # Run from somewhere else: the signpost anchors on its own location.
    run bash -c "cd / && '$APP/scripts/update.sh' --version v2.0.1"
    assert_status 0
    assert_contains "$OUT" "retired"
    assert_eq "$(tr '\n' ' ' < "$TEST_TMP/manage-args")" "update --version v2.0.1 "
}

test_an_unmigrated_install_is_told_what_to_run() {
    make_install
    run "$APP/scripts/update.sh"
    assert_status 1
    assert_contains "$OUT" "git pull && ./scripts/manage.sh migrate"
    assert_no_file "$TEST_TMP/manage-args" "manage.sh must not run before the migration"
}

test_the_signpost_does_no_update_work() {
    # Nothing but a grep and an exec: no sudo, no docker, no git, no network.
    make_install
    run "$APP/scripts/update.sh"
    assert_eq "$(grep -cv '^#' "$STUB_LOG" || true)" 0 "the signpost called a system command"
}
