# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for .github/scripts/build-host-files.sh, against a small repository of its own so the
# result does not depend on what this checkout's HEAD happens to hold.

make_repo() {
    REPO="$TEST_TMP/repo"
    mkdir -p "$REPO/.github/scripts" "$REPO/scripts/lib" "$REPO/deploy" "$REPO/internal"
    cp "$REPO_ROOT/.github/scripts/build-host-files.sh" "$REPO/.github/scripts/"
    printf '# the asset\nscripts/manage.sh\nscripts/lib/common.sh   # trailing comment\n\ndocker-compose.yml\ndeploy/Caddyfile\n' \
        > "$REPO/.github/host-files.manifest"
    printf '#!/bin/bash\necho manage\n' > "$REPO/scripts/manage.sh"
    chmod +x "$REPO/scripts/manage.sh"
    echo '# lib' > "$REPO/scripts/lib/common.sh"
    echo 'services: {}' > "$REPO/docker-compose.yml"
    echo '# alliance-manager caddyfile rev 1' > "$REPO/deploy/Caddyfile"
    echo 'package app' > "$REPO/internal/a.go"
    git -C "$REPO" init -q -b main
    git -C "$REPO" -c user.email=t@t -c user.name=t add -A
    git -C "$REPO" -c user.email=t@t -c user.name=t commit -qm one
}

build() {  # build VERSION OUT [REF]
    run "$REPO/.github/scripts/build-host-files.sh" "$@"
}

test_the_tarball_is_the_manifest_plus_two_generated_files() {
    make_repo
    build v2.0.0 "$TEST_TMP/out"
    assert_status 0
    assert_eq "$(tar -tzf "$TEST_TMP/out/host-files.tar.gz" | LC_ALL=C sort | tr '\n' ' ')" \
        ".host-files.list HOST_FILES_VERSION deploy/Caddyfile docker-compose.yml scripts/lib/common.sh scripts/manage.sh "
    assert_eq "$(tar -xzOf "$TEST_TMP/out/host-files.tar.gz" HOST_FILES_VERSION)" v2.0.0
    assert_eq "$(tar -xzOf "$TEST_TMP/out/host-files.tar.gz" .host-files.list | tr '\n' ' ')" \
        ".host-files.list HOST_FILES_VERSION deploy/Caddyfile docker-compose.yml scripts/lib/common.sh scripts/manage.sh "
    ! tar -tzf "$TEST_TMP/out/host-files.tar.gz" | grep -q internal || _fail "a file not in the manifest was shipped"
}

test_two_builds_are_byte_identical_and_the_checksum_verifies() {
    make_repo
    build v2.0.0 "$TEST_TMP/a"
    assert_status 0
    sleep 1   # a different wall-clock second must not change the bytes
    build v2.0.0 "$TEST_TMP/b"
    assert_status 0
    cmp -s "$TEST_TMP/a/host-files.tar.gz" "$TEST_TMP/b/host-files.tar.gz" || _fail "two builds differ"
    (cd "$TEST_TMP/a" && sha256sum -c --status host-files.tar.gz.sha256) || _fail "the checksum does not verify"
    assert_contains "$(cat "$TEST_TMP/a/host-files.tar.gz.sha256")" "  host-files.tar.gz"
}

test_modes_come_from_git_not_the_checkout() {
    make_repo
    chmod -x "$REPO/scripts/manage.sh"   # the checkout lost the bit; git still has it
    build v2.0.0 "$TEST_TMP/out"
    assert_status 0
    assert_eq "$(tar -tvzf "$TEST_TMP/out/host-files.tar.gz" scripts/manage.sh | cut -c1-10)" "-rwxr-xr-x"
    assert_eq "$(tar -tvzf "$TEST_TMP/out/host-files.tar.gz" docker-compose.yml | cut -c1-10)" "-rw-r--r--"
    assert_eq "$(tar -tvzf "$TEST_TMP/out/host-files.tar.gz" docker-compose.yml | awk '{print $2}')" "0/0"
}

test_builds_from_the_ref_not_the_working_tree() {
    make_repo
    local first
    first=$(git -C "$REPO" rev-parse HEAD)
    echo 'services: {changed: 1}' > "$REPO/docker-compose.yml"
    git -C "$REPO" -c user.email=t@t -c user.name=t commit -qam two
    echo 'uncommitted' > "$REPO/docker-compose.yml"
    build rehearsal-abc123 "$TEST_TMP/out" "$first"
    assert_status 0
    assert_eq "$(tar -xzOf "$TEST_TMP/out/host-files.tar.gz" docker-compose.yml)" 'services: {}'
    assert_eq "$(tar -xzOf "$TEST_TMP/out/host-files.tar.gz" HOST_FILES_VERSION)" rehearsal-abc123
}

test_refuses_a_version_that_is_a_path() {
    make_repo
    build ../escape "$TEST_TMP/out"
    assert_status 2
    assert_no_file "$TEST_TMP/out/host-files.tar.gz"
}

test_the_real_manifest_builds() {
    # This checkout's manifest, at its HEAD: every path it names exists there.
    run "$REPO_ROOT/.github/scripts/build-host-files.sh" v0.0.0-test "$TEST_TMP/real"
    if git -C "$REPO_ROOT" cat-file -e HEAD:.github/host-files.manifest 2>/dev/null; then
        assert_status 0
        tar -tzf "$TEST_TMP/real/host-files.tar.gz" | grep -qx scripts/manage.sh || _fail "manage.sh not shipped"
    fi
}
