# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for scripts/lib/registry.sh — the frozen install-registry format.
# shellcheck source=../../scripts/lib/common.sh
source "$REPO_ROOT/scripts/lib/common.sh"
# shellcheck source=../../scripts/lib/registry.sh
source "$REPO_ROOT/scripts/lib/registry.sh"

make_install() {  # make_install DIR — a directory that looks like an install
    mkdir -p "$1"
    touch "$1/docker-compose.yml"
}

test_write_then_resolve() {
    make_install "$TEST_TMP/one"
    registry_write default "$TEST_TMP/one"
    assert_file "$AM_REGISTRY_DIR/default.conf"
    run registry_resolve
    assert_status 0
    assert_eq "$OUT" "$TEST_TMP/one"
    run registry_resolve default
    assert_eq "$OUT" "$TEST_TMP/one"
}

test_file_is_one_sourceable_line() {
    make_install "$TEST_TMP/with space"
    registry_write default "$TEST_TMP/with space"
    assert_eq "$(grep -c '^APP_DIR=' "$AM_REGISTRY_DIR/default.conf")" 1
    APP_DIR=
    # shellcheck disable=SC1091
    . "$AM_REGISTRY_DIR/default.conf"
    assert_eq "$APP_DIR" "$TEST_TMP/with space"
}

test_no_entry_names_both_ways_in() {
    run registry_resolve
    assert_status 1
    assert_contains "$OUT" "install.sh"
    assert_contains "$OUT" "manage.sh migrate"
}

test_two_entries_need_a_selector() {
    make_install "$TEST_TMP/a"
    make_install "$TEST_TMP/b"
    registry_write alpha "$TEST_TMP/a"
    registry_write beta "$TEST_TMP/b"
    run registry_resolve
    assert_status 1
    assert_contains "$OUT" "alpha"
    assert_contains "$OUT" "beta"
    run registry_resolve beta
    assert_status 0
    assert_eq "$OUT" "$TEST_TMP/b"
    assert_eq "$(registry_list | tr '\n' ' ')" "alpha beta "
}

test_unknown_name_lists_the_registered_ones() {
    make_install "$TEST_TMP/a"
    registry_write alpha "$TEST_TMP/a"
    run registry_resolve gamma
    assert_status 1
    assert_contains "$OUT" "alpha"
}

test_dangling_directory_names_the_file_to_edit() {
    make_install "$TEST_TMP/moved"
    registry_write default "$TEST_TMP/moved"
    rm "$TEST_TMP/moved/docker-compose.yml"
    run registry_resolve
    assert_status 1
    assert_contains "$OUT" "moved"
    assert_contains "$OUT" "$AM_REGISTRY_DIR/default.conf"
}

test_rewrite_same_dir_is_a_no_op_different_dir_refused() {
    make_install "$TEST_TMP/a"
    make_install "$TEST_TMP/b"
    registry_write default "$TEST_TMP/a"
    run registry_write default "$TEST_TMP/a"
    assert_status 0
    run registry_write default "$TEST_TMP/b"
    assert_status 1
    assert_contains "$OUT" "already registered"
    assert_eq "$(registry_get default)" "$TEST_TMP/a" "a refused write changed the entry"
    registry_write default "$TEST_TMP/b" --replace
    assert_eq "$(registry_get default)" "$TEST_TMP/b"
}

test_refuses_values_that_would_run_when_sourced() {
    local bad
    for bad in "relative/dir" "/tmp/a\$(id)" "/tmp/a\"b" "/tmp/a\`id\`" "/tmp/a\\b"; do
        run registry_write default "$bad"
        assert_status 1 "registry_write accepted [$bad]"
    done
    run registry_write "../escape" "$TEST_TMP"
    assert_status 1 "registry_write accepted a name with a path in it"
    assert_no_file "$AM_REGISTRY_DIR/default.conf"
}

test_name_for_finds_the_entry() {
    make_install "$TEST_TMP/a"
    registry_write prod "$TEST_TMP/a"
    assert_eq "$(registry_name_for "$TEST_TMP/a")" prod
    run registry_name_for "$TEST_TMP/elsewhere"
    assert_status 1
}

test_writes_go_through_sudo_install() {
    make_install "$TEST_TMP/a"
    registry_write default "$TEST_TMP/a"
    assert_called "sudo install -m 0644"
}
