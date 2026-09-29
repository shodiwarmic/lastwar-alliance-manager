# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for scripts/install.sh, run against the stubs from an unpacked-asset layout.

# unpack_fake_asset DIR TAG — lay DIR out as the host-files asset would.
unpack_fake_asset() {
    local dir=$1 tag=$2
    mkdir -p "$dir/scripts/lib" "$dir/deploy"
    cp "$REPO_ROOT"/scripts/install.sh "$REPO_ROOT"/scripts/lib/*.sh "$dir/scripts/" 2>/dev/null || true
    mv "$dir"/scripts/common.sh "$dir"/scripts/registry.sh "$dir"/scripts/preflight.sh "$dir/scripts/lib/"
    cp "$REPO_ROOT/deploy/Caddyfile" "$dir/deploy/"
    cp "$REPO_ROOT/docker-compose.yml" "$REPO_ROOT/docker-compose.local-ocr.yml" "$REPO_ROOT/.env.example" "$dir/"
    echo "$tag" > "$dir/HOST_FILES_VERSION"
}

# The host the installer sees: Debian, 4 GB, plenty of disk, nothing listening, Docker up,
# lastwar.service absent. The docker stub plays the app: `compose up` records whether the
# marker was already in .env, and writes a setup key as the app would on first boot.
fake_host() {
    export AM_OS_RELEASE="$TEST_TMP/os-release" AM_MEMINFO="$TEST_TMP/meminfo"
    printf 'ID=debian\nVERSION_CODENAME=bookworm\n' > "$AM_OS_RELEASE"
    echo 'MemTotal:        4028000 kB' > "$AM_MEMINFO"
    stub_respond systemctl <<'SH'
case "$*" in *lastwar.service*) exit 3 ;; esac
exit 0
SH
    stub_respond openssl <<'SH'
printf '%064d\n' "$RANDOM"
SH
    stub_respond docker <<'SH'
if [ "$*" = "compose up -d" ]; then
    if grep -q '^HOST_FILES_VERSION=' .env; then echo '# up saw HOST_FILES_VERSION' >> "$STUB_LOG"; fi
    mkdir -p data && [ -f data/setup-key ] || echo 0123abcd > data/setup-key
fi
exit 0
SH
    stub_respond curl <<'SH'
case "$*" in *127.0.0.1:8080*) printf 200 ;; *) exit 7 ;; esac
SH
    APP="$TEST_TMP/install"
    unpack_fake_asset "$APP" v9.9.9
    # shellcheck source=../../scripts/install.sh
    source "$APP/scripts/install.sh"
    avail_kb() { echo $((50 * 1024 * 1024)); }
    docker_root_dir() { echo /; }
}

test_fresh_non_interactive_install() {
    fake_host
    run main --non-interactive --domain app.warmic.test
    assert_status 0

    ENV_FILE="$APP/.env"
    local key
    for key in SESSION_KEY CREDENTIAL_ENCRYPTION_KEY DATABASE_PATH STORAGE_PATH PRODUCTION HTTPS PORT \
        APP_DOMAIN COLLABORA_DOMAIN TRUSTED_PROXY_COUNT APP_VERSION OCR_BACKEND_MODE OCR_ARCHIVE_DIR \
        HOST_FILES_VERSION; do
        env_has "$key" || _fail ".env lacks $key"
    done
    assert_eq "$(env_get TRUSTED_PROXY_COUNT)" 1
    assert_eq "$(env_get APP_VERSION)" v9.9.9 "the pin is the unpacked release"
    assert_eq "$(env_get HOST_FILES_VERSION)" v9.9.9
    assert_eq "$(env_get COLLABORA_DOMAIN)" collabora.app.warmic.test
    assert_eq "$(env_get OCR_BACKEND_MODE)" cloud
    ! env_has COMPOSE_FILE || _fail "cloud OCR must not set COMPOSE_FILE"
    assert_eq "$(stat -c %a "$APP/.env")" 600 ".env holds secrets"
    assert_eq "${#SESSION_KEY}" 0 "nothing leaked into the test shell"

    grep -q '^# up saw HOST_FILES_VERSION' "$STUB_LOG" || _fail "HOST_FILES_VERSION was not in .env before compose up"
    [ "$(line_of 'docker compose pull')" -lt "$(line_of 'docker compose up -d')" ] || _fail "pull must precede up"

    assert_file "$AM_CADDYFILE"
    assert_contains "$(cat "$AM_CADDYFILE")" "app.warmic.test {"
    assert_contains "$(cat "$AM_CADDYFILE")" "collabora.app.warmic.test {"
    assert_not_contains "$(cat "$AM_CADDYFILE")" "__"
    assert_eq "$(caddyfile_rev "$AM_CADDYFILE")" 1
    assert_called "sudo -u caddy caddy validate --adapter caddyfile" "the rendered file is validated first"

    assert_eq "$(registry_get default)" "$APP"
    assert_file "$AM_BACKUP_HELPER"
    assert_eq "$(grep -c backup-lastwar "$STUB_DIR/crontab")" 1

    # Firewall: allow before enable.
    [ "$(line_of 'ufw allow 22/tcp')" -lt "$(line_of 'ufw --force enable')" ] || _fail "ufw enabled before SSH was allowed"
    assert_contains "$OUT" "0123abcd" "the setup key is printed"
    assert_contains "$OUT" "https://app.warmic.test/setup"
}

test_rerun_over_the_same_directory_duplicates_nothing() {
    fake_host
    run main --non-interactive --domain app.warmic.test
    assert_status 0
    local env_before
    env_before=$(cat "$APP/.env")
    # A completed install owns its ports now.
    stub_respond ss <<'SH'
case "${!#}" in
    "sport = :80"|"sport = :443") echo 'LISTEN 0 4096 *:80 *:* users:(("caddy",pid=5,fd=7))' ;;
    "sport = :8080"|"sport = :9980") echo 'LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:* users:(("docker-proxy",pid=6,fd=4))' ;;
esac
SH
    run main --non-interactive --domain app.warmic.test
    assert_status 0
    assert_eq "$(cat "$APP/.env")" "$env_before" ".env changed on a re-run"
    assert_eq "$(grep -c backup-lastwar "$STUB_DIR/crontab")" 1 "crontab line duplicated"
    assert_eq "$(find "$AM_REGISTRY_DIR" -name '*.conf' | wc -l)" 1
}

test_an_install_elsewhere_refuses() {
    fake_host
    mkdir -p "$TEST_TMP/other"
    registry_write default "$TEST_TMP/other"
    run main --non-interactive --domain app.warmic.test
    assert_status 1
    assert_contains "$OUT" "existing-install"
    assert_no_file "$APP/.env" "nothing is written before the checks pass"
}

test_local_ocr_and_archive() {
    fake_host
    pf_arch() { echo x86_64; }
    run main --non-interactive --domain app.warmic.test --ocr local --archive --proxy none
    assert_status 0
    ENV_FILE="$APP/.env"
    assert_eq "$(env_get OCR_BACKEND_MODE)" local
    assert_eq "$(env_get COMPOSE_FILE)" docker-compose.yml:docker-compose.local-ocr.yml
    assert_eq "$(env_get OCR_ARCHIVE_DIR)" /app/data/ocr-archive
    assert_eq "$(env_get OCR_ARCHIVE_RETENTION_DAYS)" 7
    assert_no_file "$AM_CADDYFILE" "--proxy none writes no Caddyfile"
    assert_contains "$OUT" "Option B"
}

test_non_interactive_needs_a_domain() {
    fake_host
    run main --non-interactive
    assert_status 1
    assert_contains "$OUT" "--domain"
}

test_refuses_a_placeholder_domain() {
    fake_host
    run main --non-interactive --domain app.example.com
    assert_status 1
    assert_contains "$OUT" "not a usable domain"
}

test_refuses_to_run_outside_an_asset() {
    fake_host
    rm "$APP/HOST_FILES_VERSION"
    run main --non-interactive --domain app.warmic.test
    assert_status 1
    assert_contains "$OUT" "host-files.tar.gz"
}

test_ignored_check_is_logged_in_install_log() {
    fake_host
    stub_respond ss <<'SH'
case "${!#}" in "sport = :80") echo 'LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=1,fd=6))' ;; esac
SH
    run main --non-interactive --domain app.warmic.test
    assert_status 1
    assert_contains "$OUT" "port-80"
    run main --non-interactive --domain app.warmic.test --ignore port-80
    assert_status 0
    assert_contains "$(cat "$APP/install.log")" "IGNORED port-80: port 80 is in use by nginx"
}
