# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for the host update agent (scripts/lib/update-agent.sh, installed by ensure_update_agent).
#
# The agent runs as its installed text, against a temp install, with three fakes:
#   docker     a small model of the containers (state files in $DK): image, running, a health
#              sequence, restart counts, image tags, mounts
#   manage.sh  the install's updater, playing apply's side of the protocol by scenario (files in
#              $SC): receipt phases, the .env markers, recreating the container, migrating
#   setpriv    runs the command as this user (the tests cannot change uid) and records the call
# The real apply's receipt, lock and offline mode are tested in manage_test.sh; the two meet on
# the rehearsal host (docs/RELEASING.md → rehearsing).
# shellcheck source=../../scripts/lib/common.sh
source "$REPO_ROOT/scripts/lib/common.sh"
# shellcheck source=../../scripts/lib/registry.sh
source "$REPO_ROOT/scripts/lib/registry.sh"

APP="$TEST_TMP/am"
DK="$TEST_TMP/dk"
SC="$TEST_TMP/scenario"
AGENT="$TEST_TMP/agent.sh"
STATE="$AM_STATE_DIR/default"
SPOOL="$APP/data/update"
REPO=ghcr.io/shodiwarmic/lastwar-alliance-manager
export DK SC

# The fake updater. update: what stage 1 + apply do, by scenario. apply: the recovery run of a
# previous release's apply from the bundle.
fake_manage() {
    cat <<'EOF'
#!/bin/bash
SC="$TEST_TMP/scenario"; DK="$TEST_TMP/dk"
APP=$(cd "$(dirname "$0")/.." && pwd)
mode=$1; shift
printf '%s %s %s\n' "$0" "$mode" "$*" >> "$TEST_TMP/manage.calls"
setenv() { sed -i "s|^$1=.*|$1=$2|" "$3/.env"; }
recreate() {  # recreate IMAGE HEALTHFILE
    printf '%s\n' "$1" > "$DK/cid-app.image"
    echo true > "$DK/cid-app.running"
    if [ -f "$2" ]; then cp "$2" "$DK/cid-app.health"; else echo healthy > "$DK/cid-app.health"; fi
}
case $mode in
update)
    while [ $# -gt 0 ]; do case $1 in --version) v=$2; shift 2 ;; --receipt) r=$2; shift 2 ;; *) shift ;; esac; done
    [ -f "$SC/stage1-fail" ] && { echo "Error: could not download the $v host files; nothing changed"; exit 1; }
    from=$(sed -n 's/^APP_VERSION=//p' "$APP/.env"); id=${r##*receipt-}
    schema=$(sed -n 's/^schema=//p' "$APP/data/alliance.db")
    mkdir -p "$AM_BACKUP_DIR/pinned"
    cp "$APP/data/alliance.db" "$AM_BACKUP_DIR/db_x-$id.db"
    ln -f "$AM_BACKUP_DIR/db_x-$id.db" "$AM_BACKUP_DIR/pinned/$id.db"
    rcpt() { printf 'alliance-manager update receipt 1\nid=%s\napp_dir=%s\nfrom=%s\nto=%s\ndb_backup=%s\napp_backup=x\npinned_db=%s\nschema_max=%s\nphase=%s\n' \
        "$id" "$APP" "$from" "$v" "$AM_BACKUP_DIR/db_x-$id.db" "$AM_BACKUP_DIR/pinned/$id.db" "$schema" "$1" > "$r.t"; mv "$r.t" "$r"; }
    rcpt backed-up
    setenv APP_VERSION "$v" "$APP"; rcpt armed
    [ -f "$SC/sleep" ] && sleep "$(cat "$SC/sleep")"
    if [ -f "$SC/self-rollback" ]; then setenv APP_VERSION "$from" "$APP"; echo "Error: could not pull the $v images"; exit 1; fi
    if [ -f "$SC/truncate-and-die" ]; then
        : > "$APP/scripts/manage.sh"
        kill -KILL "$(ps -o ppid= -p "$PPID" | tr -d ' ')"
        exit 137
    fi
    [ -f "$SC/collabora.image" ] && cp "$SC/collabora.image" "$APP/collabora.image"
    setenv HOST_FILES_VERSION "$v" "$APP"; rcpt marker
    [ -f "$SC/migrate" ] && sed -i "s/^schema=.*/schema=$(cat "$SC/migrate")/" "$APP/data/alliance.db"
    recreate "$REPO_IMAGE:$v" "$SC/health"
    [ -f "$SC/image" ] && cp "$SC/image" "$DK/cid-app.image"
    [ -f "$SC/collabora.flapping" ] && touch "$DK/cid-collabora.flapping"
    if [ -f "$SC/kill-agent-after-up" ]; then kill -KILL "$(ps -o ppid= -p "$PPID" | tr -d ' ')"; fi
    rcpt up
    exit 0 ;;
apply)
    while [ $# -gt 0 ]; do case $1 in --app-dir) a=$2; shift 2 ;; --target) t=$2; shift 2 ;; --staging) s=$2; shift 2 ;; *) shift ;; esac; done
    tags=$(tr '\n' ' ' < "$DK/tags" 2>/dev/null)
    echo "tags at recovery: $tags" >> "$TEST_TMP/manage.calls"
    [ -f "$SC/recovery-fails" ] && exit 1
    cp -a "$s/." "$a/"
    rm -f "$a/collabora.image" "$DK/cid-collabora.flapping"
    setenv APP_VERSION "$t" "$a"; setenv HOST_FILES_VERSION "$t" "$a"
    recreate "$REPO_IMAGE:$t" "$SC/rollback.health"
    exit 0 ;;
esac
EOF
}

# The fake docker.
fake_docker() {
    stub_respond docker <<'SH'
health() { local f="$DK/$1.health" h; h=$(head -n1 "$f" 2>/dev/null || echo healthy); [ "$(wc -l < "$f" 2>/dev/null || echo 0)" -gt 1 ] && sed -i 1d "$f"; echo "$h"; }
case $1 in
    ps)
        [ -f "$DK/app.gone" ] && exit 0
        case "$*" in *service=alliance-manager*) echo cid-app ;; *service=collabora*) echo cid-collabora ;; esac ;;
    inspect)
        fmt=$3 c=$4
        case $fmt in
            *com.docker.compose.project*) echo am ;;
            *Mounts*)
                if [ -f "$DK/mounts.json" ]; then cat "$DK/mounts.json"
                else printf '[{"Type":"bind","Source":"%s/data","Destination":"/app/data"}]\n' "$(cat "$DK/app_dir")"; fi ;;
            *'{{.State.Running}} {{if .State.Health}}'*) echo "$(cat "$DK/$c.running") $(health "$c")" ;;
            *'{{.Config.Image}}'*) cat "$DK/$c.image" ;;
            *'{{.State.Running}} {{.RestartCount}}'*)
                r=$(cat "$DK/$c.restarts" 2>/dev/null || echo 0)
                if [ -f "$DK/$c.flapping" ]; then r=$((r + 1)); echo "$r" > "$DK/$c.restarts"; fi
                echo "$(cat "$DK/$c.running") $r" ;;
            *'{{if .State.Health}}{{.State.Health.Status}}{{end}}'*) head -n1 "$DK/$c.health" ;;
            *'{{.State.Running}}'*) cat "$DK/$c.running" ;;
        esac ;;
    stop) [ -f "$DK/unstoppable" ] || echo false > "$DK/$4.running" ;;
    tag) if [ -f "$DK/absent" ] && grep -qxF "$2" "$DK/absent"; then exit 1; fi; echo "$3" >> "$DK/tags" ;;
    rmi) r=$2; [ "$r" = -f ] && r=$3; grep -vxF "$r" "$DK/tags" > "$DK/tags.new" 2>/dev/null; mv "$DK/tags.new" "$DK/tags" ;;
    images) cat "$DK/tags" 2>/dev/null ;;
    compose)
        case "$*" in
            *"config --format json"*)
                col=$(cat "$PWD/collabora.image" 2>/dev/null || echo 'collabora/code@sha256:old')
                printf '{"services":{"alliance-manager":{"image":"%s:%s"},"collabora":{"image":"%s"}}}\n' \
                    "$REPO_IMAGE" "$(sed -n 's/^APP_VERSION=//p' "$PWD/.env")" "$col" ;;
            *"ps --all --quiet alliance-manager"*) echo cid-app ;;
            *"port collabora 9980"*) cat "$DK/collabora.port" 2>/dev/null ;;
            *"up -d"*) echo true > "$DK/cid-app.running" ;;
        esac ;;
esac
exit 0
SH
}

setup_agent() {
    mkdir -p "$DK" "$SC" "$APP/scripts/lib" "$SPOOL" "$APP/uploads"
    chmod 0755 "$APP" "$APP/scripts" "$APP/scripts/lib"
    cp "$REPO_ROOT"/scripts/lib/*.sh "$APP/scripts/lib/"
    chmod 0644 "$APP"/scripts/lib/*.sh
    fake_manage > "$APP/scripts/manage.sh"
    chmod 0755 "$APP/scripts/manage.sh"
    echo 'services: {}' > "$APP/docker-compose.yml"
    echo v9.9.9 > "$APP/HOST_FILES_VERSION"
    (cd "$APP" && { find scripts -type f; echo docker-compose.yml; echo HOST_FILES_VERSION; echo .host-files.list; } | sort > .host-files.list)
    printf 'SESSION_KEY=x\nAPP_VERSION=v9.9.9\nHOST_FILES_VERSION=v9.9.9\n' > "$APP/.env"
    printf 'schema=82\nrows before the update\n' > "$APP/data/alliance.db"
    registry_write default "$APP"
    echo "$APP" > "$DK/app_dir"
    echo "$REPO:v9.9.9" > "$DK/cid-app.image"
    echo true > "$DK/cid-app.running"
    echo healthy > "$DK/cid-app.health"
    echo true > "$DK/cid-collabora.running"
    echo healthy > "$DK/cid-collabora.health"
    update_agent_text v9.9.10 | sed "s/^SPOOL_UID='1001'$/SPOOL_UID='$(id -u)'/" > "$AGENT"
    fake_docker
    ln -sf "$TESTS_DIR/lib/stubs/stub" "$STUB_DIR/bin/setpriv"
    stub_respond setpriv <<'SH'
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
SH
    stub_respond curl <<'SH'
[ -f "$DK/discovery-fails" ] && exit 22
exit 0
SH
    export REPO_IMAGE=$REPO AM_AGENT_TICK_LOCK="$TEST_TMP/tick.lock" AM_AGENT_LEASE_EVERY=1 \
        AM_AGENT_ACK_WAIT=3 AM_AGENT_HEALTH_WAIT=4 AM_AGENT_POLL=1 AM_AGENT_STABLE_FOR=1 \
        AM_AGENT_ANCESTORS_TO="$TEST_TMP" AM_AGENT_TEST_ONE_UID=1
}

# request ID [TARGET] [FROM] — what the app writes.
request() {
    printf 'alliance-manager update request 1\nid=%s\nfrom=%s\ntarget=%s\nrequested_by=admin\n' \
        "$1" "${3:-v9.9.9}" "${2:-alliance-manager v9.9.10}" > "$SPOOL/request"
}

# ack ID — the app's acknowledgement that it has stopped every writer.
ack() { printf 'alliance-manager update gate-ack 1\nid=%s\n' "$1" > "$SPOOL/gate-ack"; }

agent() { run bash "$AGENT"; }

# settle — after a run that killed the agent, let its refresher notice and exit.
settle() { sleep 1.5; }

outcome() { sed -n 's/^outcome=//p' "$SPOOL/result-$1" 2>/dev/null; }
jphase() { sed -n 's/^phase=//p' "$STATE/journal"; }
message() { sed -n 's/^message=//p' "$SPOOL/result-$1" 2>/dev/null; }

# terminal ID — the request finished cleanly: one result, nothing running, the lease gone.
terminal() {
    assert_file "$SPOOL/result-$1" "no result for $1"
    assert_no_file "$SPOOL/running" "running left behind"
    assert_no_file "$SPOOL/status" "status left behind"
    assert_eq "$(find "$SPOOL" -name 'result-*' | wc -l)" 1 "exactly one result"
    assert_eq "$(jphase)" "done"
    local pid
    pid=$(cat "$STATE/refresher.pid" 2>/dev/null || true)
    [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null || _fail "the refresher outlived the request"
}

# --- Ticks, heartbeats, noop ------------------------------------------------------------------

test_an_idle_tick_writes_the_heartbeat_and_says_nothing() {
    setup_agent
    agent
    assert_status 0
    assert_eq "$OUT" "" "an idle tick logs nothing"
    assert_eq "$(head -n1 "$SPOOL/agent")" "alliance-manager update agent 1"
    assert_eq "$(sed -n 's/^release=//p' "$SPOOL/agent")" v9.9.10
    assert_eq "$(sed -n 's/^protocol=//p' "$SPOOL/agent")" 1
    grep -q '^OWNER_UID=' "$AM_REGISTRY_DIR/default.conf" || _fail "the owner was not recorded on first sight"
}

test_an_install_that_has_not_booted_the_agent_release_is_skipped() {
    setup_agent
    rm -rf "$SPOOL"
    agent
    assert_status 0
    assert_no_file "$SPOOL/agent"
}

test_a_noop_round_trips_and_raises_no_barrier() {
    setup_agent
    request t1 noop
    agent
    assert_status 0
    assert_eq "$(outcome t1)" ok
    terminal t1
    assert_eq "$(sed -n 's/^kind=//p' "$SPOOL/result-t1")" noop
    assert_no_file "$TEST_TMP/manage.calls" "a noop runs nothing"
}

test_the_second_installs_heartbeat_is_written_before_the_first_request() {
    setup_agent
    local b="$TEST_TMP/second"
    mkdir -p "$b/data/update"
    registry_write second "$b"
    request t2 noop
    agent
    assert_status 0
    local hb started
    hb=$(sed -n 's/^seen_at=//p' "$b/data/update/agent")
    started=$(sed -n 's/^started_at=//p' "$STATE/journal")
    [ -n "$hb" ] && [ "$hb" -le "$started" ] || _fail "second's heartbeat ($hb) after default's request ($started)"
}

# --- The forward update -----------------------------------------------------------------------

test_an_update_installs_end_to_end_and_releases_what_it_held() {
    setup_agent
    request u1
    ack u1
    agent
    assert_status 0
    assert_eq "$(outcome u1)" installed "$(message u1)"
    terminal u1
    grep -q "manage.sh update --version v9.9.10 --yes --receipt $STATE/receipt-u1" "$TEST_TMP/manage.calls" \
        || _fail "the updater was not driven as expected: $(cat "$TEST_TMP/manage.calls")"
    assert_eq "$(cat "$DK/tags" 2>/dev/null)" "" "the pinned images were not released"
    assert_no_file "$STATE/rollback-u1.tar.gz"
    assert_no_file "$STATE/receipt-u1"
    assert_no_file "$AM_BACKUP_DIR/pinned/u1.db"
    assert_file "$AM_BACKUP_DIR/db_x-u1.db" "the backup itself stays in the pool"
    assert_eq "$(sed -n 's/^health=//p' "$SPOOL/result-u1")" healthy
}

test_the_lease_is_refreshed_while_the_updater_runs() {
    setup_agent
    request u2
    ack u2
    echo 4 > "$SC/sleep"
    bash "$AGENT" > "$TEST_TMP/agent.out" 2>&1 &
    local pid=$! a b
    sleep 2
    a=$(sed -n 's/^updated_at=//p' "$SPOOL/status")
    sleep 2
    b=$(sed -n 's/^updated_at=//p' "$SPOOL/status")
    wait "$pid"
    [ -n "$a" ] && [ -n "$b" ] && [ "$b" -gt "$a" ] || _fail "status was not refreshed during the run: $a then $b"
    assert_eq "$(outcome u2)" installed
}

test_no_gate_ack_refuses_before_any_change() {
    setup_agent
    request u3
    agent
    assert_status 0
    assert_eq "$(outcome u3)" refused
    assert_contains "$(message u3)" "did not stop taking changes"
    assert_no_file "$TEST_TMP/manage.calls" "the updater ran without the barrier"
    terminal u3
}

test_a_from_mismatch_is_refused() {
    setup_agent
    request u4 "alliance-manager v9.9.10" v9.9.8
    ack u4
    agent
    assert_eq "$(outcome u4)" refused
    assert_contains "$(message u4)" "APP_VERSION is v9.9.9"
    assert_no_file "$TEST_TMP/manage.calls"
}

test_a_stage1_failure_leaves_the_running_app_alone() {
    setup_agent
    request f1
    ack f1
    touch "$SC/stage1-fail"
    agent
    assert_eq "$(outcome f1)" failed
    assert_contains "$(message f1)" "nothing on the host changed"
    ! grep -q '^docker stop' "$STUB_LOG" || _fail "a healthy, unchanged app was stopped"
    assert_eq "$(cat "$DK/cid-app.running")" true
    terminal f1
    assert_eq "$(cat "$DK/tags" 2>/dev/null)" "" "nothing moved, so nothing is kept"
}

test_a_failure_apply_rolled_back_itself_stops_nothing() {
    setup_agent
    request f2
    ack f2
    touch "$SC/self-rollback"
    agent
    assert_eq "$(outcome f2)" failed
    assert_contains "$(message f2)" "put everything back"
    ! grep -q '^docker stop' "$STUB_LOG" || _fail "stopped an app apply had already put back"
    assert_eq "$(cat "$DK/cid-app.running")" true
}

test_a_wrong_image_recovers_offline_through_the_bundle() {
    setup_agent
    request f3
    ack f3
    echo "$REPO:v6.6.6" > "$SC/image"
    agent
    assert_eq "$(outcome f3)" rolled_back "$(message f3)"
    assert_contains "$(message f3)" "not $REPO:v9.9.10"
    grep -q "^$STATE/recovery-f3/scripts/manage.sh apply --app-dir $APP --target v9.9.9 --staging $STATE/recovery-f3 --offline --yes" "$TEST_TMP/manage.calls" \
        || _fail "recovery did not run the bundle's apply offline: $(cat "$TEST_TMP/manage.calls")"
    grep -q '^docker stop -t 45 cid-app' "$STUB_LOG" || _fail "the app was not stopped first"
    assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 82 "no migration ran; nothing to restore"
    assert_contains "$(message f3)" "no migration ran"
    assert_eq "$(sed -n 's/^APP_VERSION=//p' "$APP/.env")" v9.9.9
    terminal f3
    assert_file "$STATE/rollback-f3.tar.gz" "a rolled-back request's set is kept for the operator"
    assert_file "$AM_BACKUP_DIR/pinned/f3.db"
}

test_a_failed_migration_is_restored_from_the_pinned_backup() {
    setup_agent
    request f4
    ack f4
    echo 83 > "$SC/migrate"
    printf 'starting\nunhealthy\n' > "$SC/health"
    agent
    assert_eq "$(outcome f4)" rolled_back "$(message f4)"
    assert_contains "$(message f4)" "restored from the pre-update backup"
    assert_eq "$(cat "$APP/data/alliance.db")" "$(printf 'schema=82\nrows before the update')"
    assert_no_file "$APP/data/.alliance.db.restore-f4"
    terminal f4
}

test_a_gate_release_forbids_the_restore() {
    setup_agent
    request f5
    ack f5
    echo 83 > "$SC/migrate"
    echo unhealthy > "$SC/health"
    printf 'alliance-manager update gate-released 1\nid=f5\n' > "$SPOOL/gate-released"
    agent
    assert_eq "$(outcome f5)" rolled_back
    assert_contains "$(message f5)" "left alone for the operator"
    assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 83 "the database was overwritten after the gate was released"
}

test_an_unreadable_schema_restores_nothing() {
    setup_agent
    request f6
    ack f6
    echo garbage > "$SC/migrate"
    echo unhealthy > "$SC/health"
    agent
    assert_contains "$(message f6)" "could not be read"
    assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" garbage
}

test_a_changed_collabora_that_does_not_answer_recovers_with_the_old_image_held() {
    setup_agent
    request f7
    ack f7
    echo 'collabora/code@sha256:new' > "$SC/collabora.image"
    echo 9980 > "$DK/collabora.port"
    echo '127.0.0.1:9980' > "$DK/collabora.port"
    touch "$DK/discovery-fails"
    agent
    assert_eq "$(outcome f7)" rolled_back "$(message f7)"
    assert_contains "$(message f7)" "Collabora does not answer"
    grep -q 'tags at recovery: .*collabora/code:pinned-f7' "$TEST_TMP/manage.calls" \
        || _fail "the previous Collabora image was not held during recovery: $(grep tags "$TEST_TMP/manage.calls")"
}

test_a_flapping_sidecar_fails_verify() {
    setup_agent
    request f8
    ack f8
    echo 'collabora/code@sha256:new' > "$SC/collabora.image"
    touch "$SC/collabora.flapping"
    agent
    assert_eq "$(outcome f8)" rolled_back
    assert_contains "$(message f8)" "collabora did not stay up"
}

test_a_failed_recovery_with_the_database_untouched_brings_the_app_up() {
    setup_agent
    request f9
    ack f9
    echo unhealthy > "$SC/health"
    touch "$SC/recovery-fails"
    agent
    assert_eq "$(outcome f9)" failed
    assert_contains "$(message f9)" "started on its current files"
    grep -q '^docker compose up -d --pull never' "$STUB_LOG" || _fail "the app was not brought back up"
    terminal f9
}

test_a_failed_recovery_after_a_restore_needs_the_operator() {
    setup_agent
    request f10
    ack f10
    echo 83 > "$SC/migrate"
    echo unhealthy > "$SC/health"
    touch "$SC/recovery-fails"
    agent
    assert_eq "$(outcome f10)" failed
    assert_contains "$(message f10)" "left STOPPED"
    assert_eq "$(jphase)" needs-operator
    assert_eq "$(cat "$DK/cid-app.running")" false
    ! grep -q '^docker compose up' "$STUB_LOG" || _fail "started the newer binary on the restored schema"
    agent
    assert_contains "$OUT" "still needs the operator"
    assert_eq "$(sed -n 's/^journal_phase=//p' "$SPOOL/agent")" needs-operator "the heartbeat tells the app"
}

test_three_failed_sets_are_kept_and_a_fourth_evicts_the_oldest() {
    setup_agent
    echo "$REPO:v6.6.6" > "$SC/image"
    local id
    for id in k1 k2 k3 k4; do
        request "$id"
        ack "$id"
        agent
        assert_eq "$(outcome "$id")" rolled_back
        rm -f "$SPOOL/result-$id"
    done
    assert_eq "$(tr '\n' ' ' < "$STATE/kept")" "k2 k3 k4 "
    assert_no_file "$STATE/rollback-k1.tar.gz"
    assert_no_file "$AM_BACKUP_DIR/pinned/k1.db"
    assert_file "$STATE/rollback-k2.tar.gz"
    rm "$SC/image"
    request k5
    ack k5
    agent
    assert_eq "$(outcome k5)" installed
    assert_eq "$(cat "$STATE/kept")" "" "an install releases every kept set"
    assert_no_file "$STATE/rollback-k4.tar.gz"
    assert_eq "$(cat "$DK/tags" 2>/dev/null)" ""
}

# --- What it refuses --------------------------------------------------------------------------

test_the_trust_chain_refuses_a_writable_library_or_directory() {
    setup_agent
    chmod o+w "$APP/scripts/lib/common.sh"
    request r1
    ack r1
    agent
    assert_eq "$(outcome r1)" refused
    assert_contains "$(message r1)" "common.sh is group- or world-writable"
    rm "$SPOOL/result-r1"
    chmod o-w "$APP/scripts/lib/common.sh"
    chmod g+w "$APP/scripts/lib"
    request r2
    ack r2
    agent
    assert_contains "$(message r2)" "scripts/lib is group- or world-writable"
    assert_no_file "$TEST_TMP/manage.calls"
}

test_an_unsafe_request_is_refused_within_the_deadline() {
    setup_agent
    local kind start
    for kind in fifo devzero big; do
        rm -f "$SPOOL/request" "$SPOOL/status"
        case $kind in
            fifo) mkfifo "$SPOOL/request" ;;
            devzero) ln -s /dev/zero "$SPOOL/request" ;;
            big) head -c 1048576 /dev/zero | tr '\0' a > "$SPOOL/request" ;;
        esac
        start=$(date +%s)
        agent
        [ $(( $(date +%s) - start )) -lt 15 ] || _fail "$kind: the read was not bounded"
        assert_eq "$(sed -n 's/^id=//p' "$SPOOL/status")" invalid "$kind"
        assert_eq "$(sed -n 's/^state=//p' "$SPOOL/status")" refused "$kind"
        assert_no_file "$SPOOL/running" "$kind was claimed"
    done
}

test_8192_bytes_are_read_and_8193_refused() {
    setup_agent
    local head='alliance-manager update request 1\nid=b1\nfrom=v9.9.9\ntarget=noop\npad='
    { printf "$head"; head -c $((8192 - $(printf "$head" | wc -c) - 1)) /dev/zero | tr '\0' x; printf '\n'; } > "$SPOOL/request"
    assert_eq "$(wc -c < "$SPOOL/request")" 8192
    agent
    assert_eq "$(outcome b1)" ok
    rm "$SPOOL/result-b1"
    { printf "${head/b1/b2}"; head -c $((8193 - $(printf "${head/b1/b2}" | wc -c) - 1)) /dev/zero | tr '\0' x; printf '\n'; } > "$SPOOL/request"
    assert_eq "$(wc -c < "$SPOOL/request")" 8193
    agent
    assert_no_file "$SPOOL/result-b2"
    assert_contains "$(sed -n 's/^step=//p' "$SPOOL/status")" "larger than 8192"
}

test_a_traversal_id_is_refused() {
    setup_agent
    printf 'alliance-manager update request 1\nid=../../etc\nfrom=v9.9.9\ntarget=noop\n' > "$SPOOL/request"
    agent
    assert_eq "$(sed -n 's/^id=//p' "$SPOOL/status")" invalid
    assert_no_file "$SPOOL/running"
    [ -z "$(find "$TEST_TMP" -name 'result-*etc*')" ] || _fail "a result was written for a traversal id"
}

test_a_request_of_a_newer_format_is_refused() {
    setup_agent
    printf 'alliance-manager update request 2\nid=n1\nfrom=v9.9.9\ntarget=noop\n' > "$SPOOL/request"
    agent
    assert_eq "$(outcome n1)" refused
    assert_contains "$(message n1)" "format 2"
    terminal n1
}

test_a_running_app_whose_data_is_not_the_bind_is_refused() {
    setup_agent
    for m in '[{"Type":"volume","Name":"amdata","Source":"/var/lib/docker/volumes/amdata/_data","Destination":"/app/data"}]' \
        '[{"Type":"bind","Source":"'"$APP"'/data","Destination":"/app/data"},{"Type":"bind","Source":"/srv/x.db","Destination":"/app/data/alliance.db"}]' \
        '[{"Type":"bind","Source":"'"$APP"'/data","Destination":"/app/data"},{"Type":"volume","Source":"/v","Destination":"/app/data/update"}]'; do
        rm -f "$SPOOL"/result-*
        printf '%s\n' "$m" > "$DK/mounts.json"
        request m1
        ack m1
        agent
        assert_eq "$(outcome m1)" refused
        assert_contains "$(message m1)" "is not the $APP/data bind"
    done
    assert_no_file "$TEST_TMP/manage.calls"
}

test_a_symlinked_spool_is_refused() {
    setup_agent
    mv "$SPOOL" "$TEST_TMP/elsewhere"
    ln -s "$TEST_TMP/elsewhere" "$SPOOL"
    agent
    assert_contains "$OUT" "is a symbolic link"
    assert_no_file "$TEST_TMP/elsewhere/agent"
    agent
    assert_not_contains "$OUT" "symbolic link" "the same refusal is said once"
}

test_the_spool_owner_owning_the_install_is_refused() {
    setup_agent
    request o1
    ack o1
    AM_AGENT_TEST_ONE_UID=0 agent
    assert_eq "$(outcome o1)" refused
    assert_contains "$(message o1)" "owned by uid $(id -u), the app container's own"
}

test_spool_io_goes_through_setpriv() {
    setup_agent
    request s1 noop
    agent
    grep -q "^setpriv --reuid $(id -u) --regid $(id -g) --clear-groups -- sh -c" "$STUB_LOG" \
        || _fail "spool I/O did not run as the spool owner: $(grep -m3 setpriv "$STUB_LOG")"
}

# --- Kill points: every phase resumes to a terminal outcome -----------------------------------

# kill_then_resume POINT ID [SCENARIO...] — kill the agent at POINT, then tick again.
kill_then_resume() {
    local point=$1 id=$2
    shift 2
    setup_agent
    local s
    for s in "$@"; do
        case $s in *=*) printf '%s\n' "${s#*=}" > "$SC/${s%%=*}" ;; *) touch "$SC/$s" ;; esac
    done
    request "$id"
    ack "$id"
    AM_AGENT_DIE_AT=$point run bash "$AGENT"
    assert_status 137 "the agent should have been killed at $point"
    settle
    agent
    assert_status 0
    terminal "$id"
}

test_kill_at_claiming_finalises_refused() {
    kill_then_resume claiming kc
    # Killed before the rename: the request is still waiting; the next tick claims it.
    assert_eq "$(outcome kc)" installed
}

test_kill_after_the_rename_finalises_refused() {
    kill_then_resume renamed kr
    assert_eq "$(outcome kr)" refused
    assert_contains "$(message kr)" "interrupted before it was claimed"
}

test_kill_at_claimed_and_prepared_finalises_failed_with_nothing_moved() {
    local p
    for p in claimed prepared; do
        kill_then_resume "$p" "k-$p"
        assert_eq "$(outcome "k-$p")" failed
        assert_contains "$(message "k-$p")" "interrupted before anything changed"
        assert_no_file "$TEST_TMP/manage.calls"
        rm -rf "$TEST_TMP"/{am,dk,scenario,sys} "$TEST_TMP/manage.calls"
        : > "$STUB_LOG"
    done
}

test_kill_at_applying_before_the_updater_ran_is_failed_and_harmless() {
    kill_then_resume applying ka
    assert_eq "$(outcome ka)" failed
    assert_contains "$(message ka)" "nothing on the host changed"
    assert_no_file "$TEST_TMP/manage.calls"
    assert_eq "$(cat "$DK/cid-app.running")" true
}

test_kill_inside_the_updater_after_up_resumes_into_verify() {
    kill_then_resume none ku kill-agent-after-up
    assert_eq "$(outcome ku)" installed
}

test_kill_with_manage_half_copied_recovers_through_the_bundle() {
    kill_then_resume none kh truncate-and-die
    assert_eq "$(outcome kh)" rolled_back "$(message kh)"
    [ -s "$APP/scripts/manage.sh" ] || _fail "recovery did not put the truncated manage.sh back"
    assert_eq "$(grep -c " apply " "$TEST_TMP/manage.calls")" 1
    grep -q "^$STATE/recovery-kh/scripts/manage.sh apply" "$TEST_TMP/manage.calls" || _fail "not the bundle's copy"
}

test_kill_at_each_later_phase_resumes() {
    local p id
    for p in applied verifying verified finalising result-published running-removed; do
        id="k${p//-/}"
        kill_then_resume "$p" "$id"
        assert_eq "$(outcome "$id")" installed "$p: $(message "$id")"
        rm -rf "$TEST_TMP"/{am,dk,scenario,sys} "$TEST_TMP/manage.calls"
        : > "$STUB_LOG"
    done
}

test_kill_at_each_failure_phase_resumes() {
    local p id
    for p in stopping restoring restored rolling-back rolled-back; do
        id="kf${p//-/}"
        kill_then_resume "$p" "$id" migrate=83 health=unhealthy
        assert_eq "$(outcome "$id")" rolled_back "$p: $(message "$id")"
        assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 82 "$p: the database was not restored"
        rm -rf "$TEST_TMP"/{am,dk,scenario,sys} "$TEST_TMP/manage.calls"
        : > "$STUB_LOG"
    done
}

test_kill_at_each_step_of_the_restore_leaves_a_whole_database() {
    local p id
    for p in restore-copied restore-checked restore-unlinked restore-renamed; do
        id="kd${p//-/}"
        setup_agent
        printf 'not checkpointed yet\n' > "$APP/data/alliance.db-wal"
        echo 83 > "$SC/migrate"
        echo unhealthy > "$SC/health"
        request "$id"
        ack "$id"
        AM_AGENT_DIE_AT=$p run bash "$AGENT"
        assert_status 137
        settle
        # Between the kill and the next tick, the database is one whole file and no stale WAL
        # sits beside a database it does not belong to.
        local db
        db=$(cat "$APP/data/alliance.db")
        case $db in "schema=82"*|"schema=83"*) ;; *) _fail "$p: torn database: $db" ;; esac
        if [[ $db == schema=82* ]] && [ -e "$APP/data/alliance.db-wal" ]; then
            _fail "$p: the restored database has the old WAL beside it"
        fi
        agent
        assert_eq "$(outcome "$id")" rolled_back "$p: $(message "$id")"
        assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 82 "$p"
        assert_no_file "$APP/data/alliance.db-wal" "$p: the old WAL came back beside the restored database"
        rm -rf "$TEST_TMP"/{am,dk,scenario,sys} "$TEST_TMP/manage.calls"
        : > "$STUB_LOG"
    done
}

test_a_resumed_restore_that_finds_the_app_running_again_leaves_the_database() {
    setup_agent
    echo 83 > "$SC/migrate"
    echo unhealthy > "$SC/health"
    request kq
    ack kq
    AM_AGENT_DIE_AT=restoring run bash "$AGENT"
    settle
    # The operator started the app by hand meanwhile.
    echo true > "$DK/cid-app.running"
    agent
    assert_eq "$(outcome kq)" rolled_back
    assert_contains "$(message kq)" "the app was running again"
    assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 83
}

test_a_lease_gap_forbids_a_resumed_restore() {
    setup_agent
    echo 83 > "$SC/migrate"
    echo unhealthy > "$SC/health"
    request kg
    ack kg
    AM_AGENT_DIE_AT=stopping run bash "$AGENT"
    settle
    # The agent was dead for longer than the lease: the app may have released its gate.
    echo $(( $(date +%s) - 600 )) > "$STATE/lease"
    agent
    assert_contains "$(message kg)" "left alone for the operator"
    assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 83
}

test_an_interrupted_noop_is_finalised_ok() {
    setup_agent
    request kn noop
    AM_AGENT_DIE_AT=verified run bash "$AGENT"
    settle
    agent
    assert_eq "$(outcome kn)" ok
    terminal kn
}

test_killing_the_agent_stops_the_refresher() {
    setup_agent
    request kk
    ack kk
    AM_AGENT_DIE_AT=prepared run bash "$AGENT"
    local pid
    pid=$(cat "$STATE/refresher.pid")
    sleep 2.5
    ! kill -0 "$pid" 2>/dev/null || _fail "the refresher ($pid) outlived its agent"
}

test_an_orphan_running_is_finalised_refused() {
    setup_agent
    printf 'alliance-manager update request 1\nid=o2\nfrom=v9.9.9\ntarget=noop\n' > "$SPOOL/running"
    agent
    assert_eq "$(outcome o2)" refused
    terminal o2
}

test_a_journal_of_a_newer_format_is_left_for_the_operator() {
    setup_agent
    mkdir -p "$STATE"
    printf 'alliance-manager update journal 2\nid=j1\nphase=applying\n' > "$STATE/journal"
    agent
    assert_contains "$OUT" "newer than this agent reads"
    assert_eq "$(jphase)" applying
}

test_a_journal_of_this_format_from_a_fixture_resumes() {
    # The frozen format: a journal as this release writes it, from a file rather than a run.
    setup_agent
    mkdir -p "$STATE"
    printf 'alliance-manager update journal 1\nid=j2\nkind=install\nfrom=v9.9.9\nto=v9.9.10\nphase=verified\noutcome=installed\nmessage=\nchild_exit=0\nack=1\nproject=am\nimages_before=alliance-manager %s:v9.9.9;\n' "$REPO" > "$STATE/journal"
    printf 'alliance-manager update request 1\nid=j2\nfrom=v9.9.9\ntarget=alliance-manager v9.9.10\n' > "$SPOOL/running"
    agent
    assert_eq "$(outcome j2)" installed
    terminal j2
}

test_a_terminal_update_holding_the_lock_makes_the_request_wait() {
    setup_agent
    request w1 noop
    exec 8>>"$APP/.update.lock"
    flock -n 8
    agent
    exec 8>&-
    assert_contains "$OUT" "the request waits"
    assert_file "$SPOOL/request"
    agent
    assert_eq "$(outcome w1)" ok
}

test_a_restore_abandoned_after_the_wal_moved_aside_puts_it_back() {
    setup_agent
    echo 83 > "$SC/migrate"
    echo unhealthy > "$SC/health"
    request kw
    ack kw
    AM_AGENT_DIE_AT=restore-unlinked run bash "$AGENT"
    assert_status 137
    settle
    sed -i 's/^schema=.*/schema=83/' "$APP/data/alliance.db"
    assert_no_file "$APP/data/alliance.db-wal" "precondition: the WAL is aside"
    echo true > "$DK/cid-app.running"   # the operator started the app meanwhile
    printf 'not checkpointed yet\n' > "$APP/data/.alliance.db-wal.aside-kw"
    agent
    assert_contains "$(message kw)" "the app was running again"
    assert_eq "$(cat "$APP/data/alliance.db-wal")" "not checkpointed yet" "the WAL was not put back"
    assert_eq "$(sed -n 's/^schema=//p' "$APP/data/alliance.db")" 83
}
