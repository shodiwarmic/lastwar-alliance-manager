# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for scripts/lib/preflight.sh — the prerequisite loop and the individual checks.
# shellcheck source=../../scripts/lib/common.sh
source "$REPO_ROOT/scripts/lib/common.sh"
# shellcheck source=../../scripts/lib/registry.sh
source "$REPO_ROOT/scripts/lib/registry.sh"
# shellcheck source=../../scripts/lib/preflight.sh
source "$REPO_ROOT/scripts/lib/preflight.sh"

APP_DIR="$TEST_TMP/app"
mkdir -p "$APP_DIR"
PF_LOG="$APP_DIR/install.log"

# A check that fails until its remedy has run, and counts the remedies.
pf_define fixable blocking 0 remedy_fixable
check_fixable() { [ -f "$TEST_TMP/fixed" ] && return 0; PF_MSG="not fixed yet"; return 1; }
remedy_fixable() { touch "$TEST_TMP/fixed"; echo x >> "$TEST_TMP/remedies"; }

# A check whose remedy never works.
pf_define stubborn blocking 0 remedy_stubborn
check_stubborn() { PF_MSG="still broken"; return 1; }
remedy_stubborn() { echo x >> "$TEST_TMP/remedies"; }

pf_define hard-fail blocking 0
check_hard_fail() { PF_MSG="cannot be fixed here"; return 1; }

pf_define soft-fail blocking 1
check_soft_fail() { PF_MSG="acceptable if you say so"; return 1; }

pf_define heads-up advisory 1
check_heads_up() { PF_MSG="just so you know"; return 1; }

test_a_remedied_check_is_rechecked_and_passes() {
    run preflight_run fixable
    assert_status 0
    assert_eq "$(wc -l < "$TEST_TMP/remedies")" 1 "the remedy should run exactly once"
    assert_contains "$OUT" "pass 2"
}

test_an_unfixable_failure_exits_naming_its_id() {
    run preflight_run hard-fail
    assert_status 1
    assert_contains "$OUT" "hard-fail"
    assert_contains "$OUT" "cannot be fixed here"
}

test_ignore_passes_an_ignorable_check_and_logs_it() {
    IGNORE=soft-fail run preflight_run soft-fail
    assert_status 0
    assert_file "$PF_LOG"
    assert_contains "$(cat "$PF_LOG")" "IGNORED soft-fail: acceptable if you say so"
}

test_ignore_cannot_pass_a_check_that_is_not_ignorable() {
    IGNORE=hard-fail run preflight_run hard-fail
    assert_status 1
}

test_an_unignored_ignorable_failure_points_at_ignore() {
    run preflight_run soft-fail
    assert_status 1
    assert_contains "$OUT" "--ignore"
}

test_advisory_failures_do_not_block() {
    run preflight_run heads-up
    assert_status 0
    assert_contains "$OUT" "just so you know"
}

test_three_checks_at_most() {
    run preflight_run stubborn
    assert_status 1
    assert_contains "$OUT" "after three checks"
    # Fixed after the first and second checks; the third only reports.
    assert_eq "$(wc -l < "$TEST_TMP/remedies")" 2
}

test_not_root_refuses_root_and_passes_a_user() {
    pf_euid() { echo 0; }
    ! check_not_root || _fail "root passed not-root"
    assert_contains "$PF_MSG" "running as root"
    pf_euid() { echo 1000; }
    check_not_root || _fail "an ordinary user must pass not-root"
}

test_os_accepts_debian_and_ubuntu_only() {
    export AM_OS_RELEASE="$TEST_TMP/os-release"
    printf 'ID=debian\nVERSION_CODENAME=bookworm\n' > "$AM_OS_RELEASE"
    check_os || _fail "debian refused"
    printf 'ID=ubuntu\n' > "$AM_OS_RELEASE"
    check_os || _fail "ubuntu refused"
    printf 'ID=fedora\n' > "$AM_OS_RELEASE"
    ! check_os || _fail "fedora accepted"
    assert_contains "$PF_MSG" "fedora"
}

test_port_names_its_holder() {
    stub_respond ss <<'SH'
echo 'LISTEN 0      511          0.0.0.0:80        0.0.0.0:*    users:(("nginx",pid=1234,fd=6),("nginx",pid=1233,fd=6))'
SH
    ! check_port_80 || _fail "port 80 held by nginx passed"
    assert_called "sudo ss -H -ltnp sport = :80" "the holder must be read with sudo"
    assert_contains "$PF_MSG" "nginx"
    assert_contains "$PF_MSG" "--proxy none"
}

test_port_held_by_our_own_service_passes_on_a_rerun() {
    stub_respond ss <<'SH'
echo 'LISTEN 0 4096 127.0.0.1:8080 0.0.0.0:* users:(("docker-proxy",pid=99,fd=4))'
SH
    RERUN=0
    ! check_port_8080 || _fail "a held port passed on a first run"
    RERUN=1
    check_port_8080 || _fail "our own docker-proxy failed a same-directory re-run"
    stub_respond ss <<'SH'
echo 'LISTEN 0 4096 0.0.0.0:8080 0.0.0.0:* users:(("java",pid=7,fd=4))'
SH
    ! check_port_8080 || _fail "a foreign holder passed on a re-run"
}

test_free_port_passes() {
    check_port_443 || _fail "a free port failed: $PF_MSG"
}

test_dns_is_advisory_when_the_public_address_is_unknown() {
    stub_respond curl <<'SH'
exit 7
SH
    DOMAIN=app.example.org COLLAB_DOMAIN=collabora.example.org PROXY=caddy
    PF_RESULT_CLASS=blocking
    ! check_dns || _fail "dns passed with no public address"
    assert_eq "$PF_RESULT_CLASS" advisory
    assert_contains "$PF_MSG" "could not determine"
}

test_dns_mismatch_blocks_behind_caddy_and_advises_without() {
    stub_respond curl <<'SH'
echo 203.0.113.9
SH
    stub_respond getent <<'SH'
case $2 in
    app.example.org) echo '203.0.113.9     STREAM app.example.org' ;;
    *)               echo '198.51.100.1    STREAM other' ;;
esac
SH
    DOMAIN=app.example.org COLLAB_DOMAIN=collabora.example.org
    PROXY=caddy PF_RESULT_CLASS=blocking
    ! check_dns || _fail "a name pointing elsewhere passed"
    assert_eq "$PF_RESULT_CLASS" blocking
    assert_contains "$PF_MSG" "collabora.example.org → 198.51.100.1"
    assert_not_contains "$PF_MSG" "app.example.org →"
    assert_contains "$PF_MSG" "--ignore dns"
    PROXY=none PF_RESULT_CLASS=blocking
    ! check_dns || true
    assert_eq "$PF_RESULT_CLASS" advisory

    stub_respond getent <<'SH'
echo '203.0.113.9     STREAM x'
SH
    PROXY=caddy
    check_dns || _fail "both names point here but dns failed: $PF_MSG"
}

test_arch_ocr_refuses_local_on_arm() {
    pf_arch() { echo aarch64; }
    OCR=local
    ! check_arch_ocr || _fail "local OCR accepted on aarch64"
    OCR=cloud
    check_arch_ocr || _fail "cloud OCR refused on aarch64"
    pf_arch() { echo x86_64; }
    OCR=local
    check_arch_ocr || _fail "local OCR refused on x86_64"
}

test_tools_installs_packages_not_binary_names() {
    pf_tool_present() { case $1 in ss|gpg|fail2ban) return 1 ;; *) return 0 ;; esac; }
    PF_TOOLS="curl ss gpg fail2ban"
    ! check_tools || _fail "missing tools passed"
    assert_contains "$PF_MSG" "ss gpg fail2ban"
    remedy_tools
    assert_called "apt-get install -y iproute2 gnupg fail2ban"
}

test_disk_blocks_only_when_images_cannot_fit() {
    docker_root_dir() { echo /; }
    avail_kb() { echo $((1 * 1024 * 1024)); }
    PF_RESULT_CLASS=blocking
    ! check_disk || _fail "1 GB free passed"
    assert_eq "$PF_RESULT_CLASS" blocking
    avail_kb() { echo $((3 * 1024 * 1024)); }
    PF_RESULT_CLASS=blocking
    ! check_disk || _fail "3 GB free raised no advisory"
    assert_eq "$PF_RESULT_CLASS" advisory
    avail_kb() { echo $((50 * 1024 * 1024)); }
    check_disk || _fail "50 GB free failed"
}

test_memory_is_advisory() {
    export AM_MEMINFO="$TEST_TMP/meminfo"
    echo 'MemTotal:        1015808 kB' > "$AM_MEMINFO"
    ! check_memory || _fail "1 GB passed"
    assert_eq "${PF_CLASS[memory]}" advisory
    echo 'MemTotal:        1995000 kB' > "$AM_MEMINFO"
    check_memory || _fail "a 2 GB machine failed"
}

test_existing_install_elsewhere_blocks_this_one_passes() {
    mkdir -p "$TEST_TMP/other"
    registry_write default "$TEST_TMP/other"
    ! check_existing_install || _fail "an install elsewhere passed"
    assert_contains "$PF_MSG" "$TEST_TMP/other"
    assert_contains "$PF_MSG" "manage.sh update"
    registry_write default "$APP_DIR" --replace
    check_existing_install || _fail "a re-run over this directory failed"
}

test_caddyfile_ours_or_none() {
    check_caddyfile || _fail "no Caddyfile failed"
    mkdir -p "$(dirname "$AM_CADDYFILE")"
    printf ':80 {\n    file_server\n}\n' > "$AM_CADDYFILE"
    ! check_caddyfile || _fail "a foreign Caddyfile passed"
    printf '# alliance-manager caddyfile rev 1\n' > "$AM_CADDYFILE"
    check_caddyfile || _fail "our own Caddyfile failed"
}

test_legacy_service_blocks_with_the_way_forward() {
    stub_respond systemctl <<'SH'
[ "$3" = lastwar.service ] && exit 0
exit 3
SH
    ! check_legacy_service || _fail "a running lastwar.service passed"
    assert_contains "$PF_MSG" "/var/lib/lastwar"
    assert_contains "$PF_MSG" "v1.1.0"
}
