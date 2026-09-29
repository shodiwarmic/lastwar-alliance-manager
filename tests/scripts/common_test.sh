# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for scripts/lib/common.sh.
# shellcheck source=../../scripts/lib/common.sh
source "$REPO_ROOT/scripts/lib/common.sh"

setup_env() {
    export APP_DIR="$TEST_TMP/app"
    mkdir -p "$APP_DIR"
}

test_env_set_appends_an_absent_key_after_a_missing_newline() {
    setup_env
    printf 'A=1\nB=2' > "$APP_DIR/.env"   # .env.example really does end like this
    env_set NEW value
    assert_eq "$(cat "$APP_DIR/.env")" "$(printf 'A=1\nB=2\nNEW=value')"
}

test_env_set_replaces_in_place() {
    setup_env
    printf 'A=1\nKEY=old\nB=2\nKEY=second\n' > "$APP_DIR/.env"
    chmod 600 "$APP_DIR/.env"
    env_set KEY 'n/e&w\1'
    assert_eq "$(cat "$APP_DIR/.env")" "$(printf 'A=1\nKEY=n/e&w\\1\nB=2\nKEY=second')"
    assert_eq "$(stat -c %a "$APP_DIR/.env")" 600 "env_set changed the file mode"
}

test_env_set_leaves_a_commented_key_alone() {
    setup_env
    printf '# KEY=example\nA=1\n' > "$APP_DIR/.env"
    env_set KEY real
    assert_eq "$(cat "$APP_DIR/.env")" "$(printf '# KEY=example\nA=1\nKEY=real')"
    assert_eq "$(env_get KEY)" real
}

test_env_get_unquoted_value_with_spaces() {
    setup_env
    # The documented TRUSTED_ORIGINS shape — the line `source .env` dies on.
    printf 'TRUSTED_ORIGINS=app.example.com, localhost:8080, 127.0.0.1:8080\n' > "$APP_DIR/.env"
    assert_eq "$(env_get TRUSTED_ORIGINS)" "app.example.com, localhost:8080, 127.0.0.1:8080"
}

test_env_get_strips_one_pair_of_quotes() {
    setup_env
    printf 'D="quoted value"\nS='"'"'single'"'"'\nE=\nH="a"b"\n' > "$APP_DIR/.env"
    assert_eq "$(env_get D)" "quoted value"
    assert_eq "$(env_get S)" "single"
    assert_eq "$(env_get E)" ""
    assert_eq "$(env_get H)" 'a"b'
}

test_env_get_absent_key_is_empty_and_succeeds() {
    setup_env
    printf 'A=1\n# B=2\n' > "$APP_DIR/.env"
    run env_get B
    assert_status 0
    assert_eq "$OUT" ""
    run env_get MISSING_FILE_KEY
    assert_status 0
}

test_env_has_sees_an_empty_value() {
    setup_env
    printf 'OCR_ARCHIVE_DIR=\n' > "$APP_DIR/.env"
    env_has OCR_ARCHIVE_DIR || _fail "env_has missed an empty-valued key"
    ! env_has OCR_BACKEND_MODE || _fail "env_has found an absent key"
}

test_env_ensure_never_overwrites() {
    setup_env
    printf 'A=mine\n' > "$APP_DIR/.env"
    env_ensure A theirs
    env_ensure B added
    assert_eq "$(env_get A)" mine
    assert_eq "$(env_get B)" added
}

test_resolve_latest_release_reads_the_tag() {
    stub_respond curl <<'SH'
cat <<'JSON'
{
  "url": "https://api.github.com/repos/shodiwarmic/lastwar-alliance-manager/releases/1",
  "tag_name": "v1.1.0",
  "name": "v1.1.0"
}
JSON
SH
    assert_eq "$(resolve_latest_release)" v1.1.0
    assert_called "curl -fsS --max-time 20 https://api.github.com/repos/shodiwarmic/lastwar-alliance-manager/releases/latest"
}

test_resolve_latest_release_404_is_empty_not_an_error() {
    stub_respond curl <<'SH'
echo 'curl: (22) The requested URL returned error: 404' >&2
exit 22
SH
    run resolve_latest_release
    assert_status 0
    assert_eq "$OUT" ""
}

test_resolve_latest_release_without_curl_is_empty() {
    local bin="$TEST_TMP/nocurl"
    mkdir -p "$bin"
    ln -s "$(command -v sed)" "$bin/sed"
    ln -s "$(command -v head)" "$bin/head"
    PATH="$bin" run resolve_latest_release
    assert_status 0
    assert_eq "$OUT" ""
}

test_version_helpers() {
    is_release_tag v2.0.0 || _fail "v2.0.0 is a release tag"
    ! is_release_tag rehearsal-abc123 || _fail "a rehearsal tag is not a release tag"
    ! is_release_tag 2.0.0 || _fail "a tag needs its v"
    version_lt v1.1.0 v2.0.0 || _fail "v1.1.0 < v2.0.0"
    version_lt v2.0.9 v2.0.10 || _fail "v2.0.9 < v2.0.10 (numeric, not lexical)"
    ! version_lt v2.0.0 v2.0.0 || _fail "equal is not less"
    ! version_lt v3.0.0 v2.9.9 || _fail "v3.0.0 is not < v2.9.9"
}

test_caddyfile_rev() {
    printf '# alliance-manager caddyfile rev 3\nexample.com {\n}\n' > "$TEST_TMP/marked"
    printf 'example.com {\n}\n' > "$TEST_TMP/unmarked"
    assert_eq "$(caddyfile_rev "$TEST_TMP/marked")" 3
    assert_eq "$(caddyfile_rev "$TEST_TMP/unmarked")" 0
    assert_eq "$(caddyfile_rev "$TEST_TMP/absent")" 0
}

test_domain_checks() {
    valid_domain app.example.org || _fail "a plain host name is valid"
    valid_domain 10.7.1.16:8080 || _fail "host:port is valid"
    ! valid_domain 'a.com|x' || _fail "a pipe must not reach the Caddyfile"
    ! valid_domain 'a.com { }' || _fail "spaces and braces are not a domain"
    ! valid_domain '' || _fail "empty is not a domain"
    placeholder_domain '' || _fail "empty is a placeholder"
    placeholder_domain app.example.com || _fail ".env.example's value is a placeholder"
    ! placeholder_domain app.warmic.org || _fail "a real domain is not a placeholder"
}

test_download_asset_verifies_the_checksum() {
    local src="$TEST_TMP/src" dest="$TEST_TMP/dl"
    mkdir -p "$src" "$dest"
    echo payload > "$src/host-files.tar.gz"
    (cd "$src" && sha256sum host-files.tar.gz > host-files.tar.gz.sha256)
    export SRC="$src"
    stub_respond curl <<'SH'
out=; url=
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift 2 ;; http*) url=$1; shift ;; *) shift ;; esac; done
cp "$SRC/${url##*/}" "$out"
SH
    run download_asset v2.0.0 "$dest"
    assert_status 0
    assert_called "curl -fsSL --max-time 300 -o $dest/host-files.tar.gz https://github.com/shodiwarmic/lastwar-alliance-manager/releases/download/v2.0.0/host-files.tar.gz"

    echo tampered > "$src/host-files.tar.gz"
    run download_asset v2.0.0 "$dest"
    assert_status 1
    assert_contains "$OUT" "does not match"
}

test_unpack_asset_rejects_something_else() {
    mkdir -p "$TEST_TMP/t/scripts"
    echo x > "$TEST_TMP/t/scripts/other.sh"
    tar -czf "$TEST_TMP/bad.tar.gz" -C "$TEST_TMP/t" .
    run unpack_asset "$TEST_TMP/bad.tar.gz" "$TEST_TMP/out"
    assert_status 1
    assert_contains "$OUT" "not a host-files asset"

    echo v9.9.9 > "$TEST_TMP/t/HOST_FILES_VERSION"
    echo '#!/bin/bash' > "$TEST_TMP/t/scripts/manage.sh"
    tar -czf "$TEST_TMP/good.tar.gz" -C "$TEST_TMP/t" .
    run unpack_asset "$TEST_TMP/good.tar.gz" "$TEST_TMP/out2"
    assert_status 0
    assert_file "$TEST_TMP/out2/scripts/manage.sh"
}
