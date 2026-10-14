#!/bin/bash
# shellcheck shell=bash disable=SC2034  # the J_* journal fields are read through ${!name}
# scripts/lib/update-agent.sh — the host update agent: installs a release an administrator chose
# on the Admin page. TEMPLATE: ensure_update_agent (lib/common.sh) substitutes the @...@ values
# and writes the result to /usr/local/bin/alliance-manager-update.sh, root-owned, where a
# systemd timer runs it every 30 seconds. Nothing executes this file from an install directory.
#
# ── What it trusts, and why ─────────────────────────────────────────────────────────────────
# It runs as root and does three kinds of thing per registered install:
#   * reads and writes the install's update spool, $APP_DIR/data/update/ — which the APP owns
#     (uid 1001 inside its container) — only as that uid, through setpriv, on a directory
#     descriptor root opened after refusing symlinks. A planted symlink or FIFO can reach only
#     what the app could already reach. Reads are bounded (size, lines, a deadline); writes are a
#     temp file then a rename, never an append. Nothing in the spool is chowned.
#   * executes the install's own scripts/manage.sh — the forward update only, after checking the
#     whole chain it sources (owner, writability, no symlinks, ancestors) — and, to recover, the
#     PREVIOUS release's manage.sh from a root-owned bundle it made before the update.
#   * runs docker, sqlite3 and the restore of the database itself.
# Its state — the journal, the claimed request's copy, receipts, the rollback bundle — is in
# @STATE_DIR@/<name>/, root-owned, which the app never sees.
#
# ── The request's state machine ─────────────────────────────────────────────────────────────
# Each phase is written to the journal (atomically, synced) BEFORE the step it names starts, so
# a kill at any point leaves a phase that says what might have happened. A later tick that finds
# a journal not at `done` resumes from it, from the facts (the .env markers, the containers, the
# receipt apply wrote), never from the assumption that a trap ran. The kill-point tests
# (tests/scripts/update_agent_test.sh) kill the agent at every row and require a terminal outcome.
#
#   phase           written                       a later tick that finds it
#   ─────────────── ───────────────────────────── ──────────────────────────────────────────────
#   claiming        before mv request running     finalise refused: interrupted before claimed
#   claimed         after the rename + hash       finalise failed: nothing changed
#   prepared        bundle, pins, ids journaled   finalise failed: nothing changed
#   applying        before the updater starts     reconcile: markers on `to` + receipt → Verify;
#                                                 markers on `from`, no receipt → failed,
#                                                 app untouched; anything else → stopping
#   applied         the updater's exit recorded   exit 0 → Verify; else Fail
#   verifying       before the health wait        Verify again
#   stopping        before the app is stopped     stop, then the rest of Fail
#   restoring       before the guarded restore    the guarded restore again (it re-checks all)
#   restored        the database is back          recovery
#   rolling-back    before the recovery apply     the recovery apply again (idempotent)
#   rolled-back     the recovery's exit recorded  Verify the previous release, then finalise
#   finalising      the outcome is decided        finalise again (idempotent)
#   needs-operator  recovery failed, db restored  report at warning every tick; nothing else
#   done            finalised                     nothing
#
# Outcomes (result-<id>): installed, rolled_back, failed, refused, and ok (a noop request).
#
# FROZEN, read across releases: the request, status, result-<id>, gate-ack, gate-released, the
# heartbeat, the journal and the receipt each carry a format number on their first line. Changes
# are additive; a reader meeting a higher number refuses. A later release's agent must resume a
# journal this one wrote.

set -o pipefail
umask 077

# ── Baked in by ensure_update_agent ─────────────────────────────────────────────────────────
AGENT_RELEASE='@AGENT_RELEASE@'
REGISTRY_DIR='@REGISTRY_DIR@'
BACKUP_DIR='@BACKUP_DIR@'
STATE_DIR='@STATE_DIR@'
# The app container's user (Dockerfile: useradd -u 1001). The spool's owner must be it, and it
# must not be anybody on this host.
SPOOL_UID='@SPOOL_UID@'
PROTOCOL=1
APP_IMAGE_REPO='ghcr.io/shodiwarmic/lastwar-alliance-manager'

# Test seams: nothing in production sets these.
TICK_LOCK=${AM_AGENT_TICK_LOCK:-/run/lock/alliance-manager-update.lock}
LEASE_EVERY=${AM_AGENT_LEASE_EVERY:-5}
LEASE_STALE=${AM_AGENT_LEASE_STALE:-120}
ACK_WAIT=${AM_AGENT_ACK_WAIT:-60}
HEALTH_WAIT=${AM_AGENT_HEALTH_WAIT:-300}
POLL=${AM_AGENT_POLL:-5}
STABLE_FOR=${AM_AGENT_STABLE_FOR:-30}
CHILD_TIMEOUT=${AM_AGENT_CHILD_TIMEOUT:-30m}
# The ancestor walk's top (the tests' temp directories sit under the world-writable /tmp).
ANCESTORS_TO=${AM_AGENT_ANCESTORS_TO:-/}
KEEP_SETS=3
KEEP_DAYS=30

# ── Logging ─────────────────────────────────────────────────────────────────────────────────
# systemd reads the <N> prefix as the priority; the unit keeps notice and above, so an idle tick
# (which says nothing) costs the journal nothing.
say()  { printf '<5>%s\n' "${NAME:+$NAME: }$*"; [ -n "${LOG:-}" ] && printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" >> "$LOG"; return 0; }
loud() { printf '<4>%s\n' "${NAME:+$NAME: }$*"; [ -n "${LOG:-}" ] && printf '%s WARNING %s\n' "$(date -u +%FT%TZ)" "$*" >> "$LOG"; return 0; }

# die_at POINT — a kill point for the tests (AM_AGENT_DIE_AT); nothing in production sets it.
die_at() { [ "${AM_AGENT_DIE_AT:-}" = "$1" ] && kill -KILL $$; return 0; }

now() { date +%s; }

# ── Small file helpers ──────────────────────────────────────────────────────────────────────

# kv FILE KEY — the value of KEY in a key=value file (first match), or nothing.
kv() { sed -n "s/^$2=//p" "$1" 2>/dev/null | head -n1; }

# write_atomic FILE — stdin to FILE through a synced temp file beside it and a rename.
write_atomic() {
    local tmp
    tmp=$(mktemp "$1.XXXXXX") || return 1
    cat > "$tmp" && sync -- "$tmp" 2>/dev/null; mv -f -- "$tmp" "$1" && sync -- "$(dirname "$1")" 2>/dev/null
    return 0
}

# env_val KEY — a key of the install's .env, read with grep, never sourced (lib/common.sh).
env_val() {
    local line
    line=$(grep -m1 "^$1=" "$APP_DIR/.env" 2>/dev/null) || return 0
    line=${line#*=}
    if [[ $line == \"*\" || $line == \'*\' ]] && [ ${#line} -ge 2 ]; then
        line=${line:1:${#line}-2}
    fi
    printf '%s' "$line"
}

# ── The journal ─────────────────────────────────────────────────────────────────────────────
# One key=value file per install, rewritten whole for each change. J_* hold it in memory.

JOURNAL_KEYS="id kind from to phase outcome message child_exit ack healthy_seen lease_gap db_restored project images_before images_after restore_note started_at"

journal_load() {
    local k
    for k in $JOURNAL_KEYS; do printf -v "J_$k" '%s' ''; done
    J_format=''
    [ -f "$JOURNAL" ] || return 1
    J_format=$(sed -n '1s/^alliance-manager update journal \([0-9][0-9]*\)$/\1/p' "$JOURNAL")
    for k in $JOURNAL_KEYS; do printf -v "J_$k" '%s' "$(kv "$JOURNAL" "$k")"; done
    return 0
}

journal_save() {
    local k v
    {
        printf 'alliance-manager update journal %s\n' "$PROTOCOL"
        for k in $JOURNAL_KEYS; do
            v="J_$k"
            printf '%s=%s\n' "$k" "${!v}"
        done
    } | write_atomic "$JOURNAL"
}

# phase P — journal the phase, then reach the kill point named after it.
phase() {
    J_phase=$1
    journal_save
    die_at "$1"
}

# ── The spool, as its owner ─────────────────────────────────────────────────────────────────
# SPFD is a descriptor on $APP_DIR/data/update opened by root after refusing symlinks; the child
# runs as the spool's uid:gid and works relative to it (/proc/self/fd/N), so no ancestor of the
# install directory — a 0700 home directory, say — has to be traversable by that uid.

as_spool() { setpriv --reuid "$SPOOL_UID" --regid "$SPOOL_GID" --clear-groups -- "$@"; }

# spool_sh SCRIPT [ARG...] — run a sh script as the spool owner, inside the spool.
spool_sh() {
    local script=$1
    shift
    as_spool sh -c 'cd "/proc/self/fd/$0" || exit 97; '"$script" "$SPFD" "$@"
}

spool_open() {
    local d="$APP_DIR/data" s="$APP_DIR/data/update"
    SPFD=''
    if [ -L "$d" ] || [ -L "$s" ]; then
        TRUST_REASON="$d or $s is a symbolic link"
        return 1
    fi
    [ -d "$s" ] || return 2      # the app has not booted on a release with the agent yet
    exec {SPFD}<"$s" || { TRUST_REASON="could not open $s"; return 1; }
    # -L: /proc/self/fd/N is a link owned by whoever holds the descriptor (root); its target is
    # the directory.
    SPOOL_GID=$(stat -L -c %g "/proc/self/fd/$SPFD")
    local owner
    owner=$(stat -L -c %u "/proc/self/fd/$SPFD")
    if [ "$owner" != "$SPOOL_UID" ]; then
        TRUST_REASON="$s is owned by uid $owner, not the app's uid $SPOOL_UID"
        return 1
    fi
    return 0
}

spool_close() { [ -n "${SPFD:-}" ] && exec {SPFD}<&-; SPFD=''; }

# spool_put NAME — stdin to spool/NAME: a fresh temp file the spool owner creates, then a rename
# (which replaces a symlink at NAME rather than following it).
spool_put() {
    spool_sh 'umask 022; t=$(mktemp .agent-XXXXXX) || exit 1; cat > "$t" && mv -fT -- "$t" "$1" || { rm -f -- "$t"; exit 1; }' "$1"
}

spool_rm() { spool_sh 'rm -f -- "$@"' "$@"; }

# spool_exists NAME — a regular file, not a link.
spool_exists() { spool_sh 'test -f "$1" && ! test -L "$1"' "$1"; }

# spool_present NAME — anything at all by that name: a FIFO or a link is still a request to
# refuse.
spool_present() { spool_sh 'test -e "$1" || test -L "$1"' "$1"; }

# spool_read NAME MAX — print spool/NAME if it is a regular file (not a link, not a FIFO) of at
# most MAX bytes, read within five seconds. Fails (status 3) when it is larger.
spool_read() {
    spool_sh 'test -f "$1" && ! test -L "$1" && ! test -p "$1" || exit 2
        out=$(timeout 5 head -c "$(($2 + 1))" -- "$1" && printf x) || exit 2
        out=${out%x}
        [ ${#out} -le "$2" ] || exit 3
        printf %s "$out"' "$1" "$2"
}

# ── Trust ───────────────────────────────────────────────────────────────────────────────────

# path_trusted PATH — owned by root or the install's owner, not group- or world-writable, not a
# symbolic link.
path_trusted() {
    local p=$1 u m
    if [ -L "$p" ]; then TRUST_REASON="$p is a symbolic link"; return 1; fi
    [ -e "$p" ] || { TRUST_REASON="$p is missing"; return 1; }
    u=$(stat -c %u "$p") m=$(stat -c %a "$p")
    if [ "$u" != 0 ] && [ "$u" != "$OWNER_UID" ]; then
        TRUST_REASON="$p is owned by uid $u, not root or the install's owner ($OWNER_UID)"
        return 1
    fi
    if (( 8#$m & 8#022 )); then
        TRUST_REASON="$p is group- or world-writable (mode $m)"
        return 1
    fi
    return 0
}

# install_trusted [recovery] — the install may be acted on. The ancestors of $APP_DIR are
# root's or the owner's and not writable by others (sudo's rule for what it will run), and the
# spool owner is nobody on this host. For a forward update the whole script chain manage.sh
# sources must be intact; a journaled recovery runs the bundle's copy instead, so a missing or
# half-copied installed script is what it repairs, not a reason to refuse it.
install_trusted() {
    local mode=${1:-forward} p d
    TRUST_REASON=''
    d=$(dirname "$APP_DIR")
    while :; do
        path_trusted "$d" || return 1
        [ "$d" = / ] || [ "$d" = "$ANCESTORS_TO" ] && break
        d=$(dirname "$d")
    done
    path_trusted "$APP_DIR" || return 1
    # (AM_AGENT_TEST_ONE_UID: the tests run every file as one uid.)
    if [ "$(stat -c %u "$APP_DIR")" = "$SPOOL_UID" ] && [ "${AM_AGENT_TEST_ONE_UID:-0}" != 1 ]; then
        TRUST_REASON="the install directory is owned by uid $SPOOL_UID, the app container's own"
        return 1
    fi
    if getent passwd "$SPOOL_UID" >/dev/null 2>&1; then
        local shell
        shell=$(getent passwd "$SPOOL_UID" | cut -d: -f7)
        case $shell in */nologin|*/false|'') ;; *)
            TRUST_REASON="uid $SPOOL_UID is a login account on this host ($(getent passwd "$SPOOL_UID" | cut -d: -f1)); the update spool cannot be told apart from it"
            return 1 ;;
        esac
    fi
    [ "$mode" = recovery ] && return 0
    for p in "$APP_DIR/scripts" "$APP_DIR/scripts/lib" "$APP_DIR/scripts/manage.sh" "$APP_DIR"/scripts/lib/*.sh; do
        path_trusted "$p" || return 1
    done
    return 0
}

# owner_record — the install's expected owner: OWNER_UID in its registry entry (an additive
# key; install.sh, apply and this agent's first sight of the install write it), else the
# directory's owner now, recorded.
owner_record() {
    OWNER_UID=$(sed -n 's/^OWNER_UID="\{0,1\}\([0-9][0-9]*\)"\{0,1\}$/\1/p' "$REGISTRY_DIR/$NAME.conf" | head -n1)
    if [ -z "$OWNER_UID" ]; then
        OWNER_UID=$(stat -c %u "$APP_DIR")
        printf 'OWNER_UID="%s"\n' "$OWNER_UID" >> "$REGISTRY_DIR/$NAME.conf"
    fi
}

# ── Heartbeat, status, results ──────────────────────────────────────────────────────────────

# heartbeat — the agent's release, its protocol, the time, and the phase of this install's last
# request (needs-operator means the app should offer nothing until the operator has acted).
heartbeat() {
    printf 'alliance-manager update agent %s\nrelease=%s\nprotocol=%s\nseen_at=%s\njournal_phase=%s\n' \
        "$PROTOCOL" "$AGENT_RELEASE" "$PROTOCOL" "$(now)" "$(kv "$STATE_DIR/$NAME/journal" phase)" | spool_put agent
}

# publish_status — the lease: status with this request's id and a fresh updated_at. Only the
# refresher calls it once a run has started; the main loop's progress reaches it through the
# step file.
publish_status() {
    local step=''
    [ -f "$STATE/step" ] && step=$(head -c 200 "$STATE/step")
    printf 'alliance-manager update status %s\nid=%s\nkind=%s\nstate=%s\nstep=%s\nfrom=%s\nto=%s\nupdated_at=%s\n' \
        "$PROTOCOL" "$J_id" "$J_kind" "$J_phase" "$step" "$J_from" "$J_to" "$(now)" | spool_put status
}

step() { printf '%s' "$*" > "$STATE/step"; say "$*"; }

# publish_log — the last lines of this request's log, for the Admin page.
publish_log() {
    [ -f "$LOG" ] && tail -n 60 "$LOG" | spool_put "update-$J_id.log"
    return 0
}

# ── The lease refresher ─────────────────────────────────────────────────────────────────────
# A tracked child that rewrites status every LEASE_EVERY seconds for the whole transaction — a
# 20-minute pull included — so the app's write gate holds; and the last refresh time, so a gap
# (a dead agent) is visible to a resumed run. It dies with the agent: it watches its parent, and
# the agent's traps and finalise kill it by its recorded pid. It never holds the install lock.

refresher_start() {
    refresher_stop
    local parent=$$
    (
        exec 9>&-
        trap 'exit 0' TERM
        local last
        while kill -0 "$parent" 2>/dev/null; do
            journal_load >/dev/null 2>&1 || true
            last=$(cat "$STATE/lease" 2>/dev/null || true)
            if [ -n "$last" ] && [ $(( $(now) - last )) -gt "$LEASE_STALE" ]; then
                printf '%s\n' "$(now)" > "$STATE/lease-gap"
            fi
            publish_status >/dev/null 2>&1 || true
            publish_log
            now > "$STATE/lease.tmp" && mv -f "$STATE/lease.tmp" "$STATE/lease"
            sleep "$LEASE_EVERY"
        done
    ) &
    REFRESHER=$!
    printf '%s\n' "$REFRESHER" > "$STATE/refresher.pid"
}

refresher_stop() {
    local pid=${REFRESHER:-}
    [ -z "$pid" ] && [ -n "${STATE:-}" ] && pid=$(cat "$STATE/refresher.pid" 2>/dev/null || true)
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null
        wait "$pid" 2>/dev/null
    fi
    REFRESHER=''
    [ -n "${STATE:-}" ] && rm -f "$STATE/refresher.pid"
    return 0
}

# lease_gap_seen — the lease lapsed at some point in this transaction: a gap the refresher saw,
# or the gap between the last refresh and now (evaluated before the lease is renewed).
lease_gap_seen() {
    local last
    [ -f "$STATE/lease-gap" ] && return 0
    last=$(cat "$STATE/lease" 2>/dev/null || true)
    [ -n "$last" ] && [ $(( $(now) - last )) -gt "$LEASE_STALE" ]
}

# gate_marker NAME — spool/NAME (gate-ack or gate-released) names this request.
gate_marker() {
    local body
    body=$(spool_read "$1" 512) || return 1
    [ "$(printf '%s\n' "$body" | sed -n '1s/^alliance-manager update [a-z-]* \([0-9][0-9]*\)$/\1/p')" = 1 ] || return 1
    [ "$(printf '%s\n' "$body" | sed -n 's/^id=//p' | head -n1)" = "$J_id" ]
}

# ── Docker ──────────────────────────────────────────────────────────────────────────────────

# cid SERVICE — the container of SERVICE in the journaled compose project, found by compose's
# own labels, so neither a truncated compose file nor a stopped container can hide it.
cid() {
    docker ps -aq --filter "label=com.docker.compose.project=$J_project" \
        --filter "label=com.docker.compose.service=$1" 2>/dev/null | head -n1
}

# stack_images — "service image" per line, for the compose files now in $APP_DIR.
stack_images() {
    (cd "$APP_DIR" && docker compose config --format json) \
        | jq -r '.services | to_entries[] | select(.value.image != null) | "\(.key) \(.value.image)"'
}

# image_for LIST SERVICE — the image of SERVICE in a journaled "service image;..." list.
image_for() { printf '%s' "$1" | tr ';' '\n' | sed -n "s/^$2 //p" | head -n1; }

# pin_ref IMAGE — the tag that holds IMAGE for this request: repo:pinned-<id>, the repository
# being IMAGE without a digest or a tag (a registry's :port is not a tag).
pin_ref() {
    local ref=${1%%@*} last
    last=${ref##*/}
    case $last in *:*) ref=${ref%:*} ;; esac
    printf '%s:pinned-%s' "$ref" "$J_id"
}

# ── One request ─────────────────────────────────────────────────────────────────────────────

# result OUTCOME MESSAGE — decide the outcome, and finalise.
result() {
    J_outcome=$1
    J_message=$2
    phase finalising
    finalise
}

# finalise — idempotent: publish the result if it is not there yet, release what the outcome
# releases, stop the lease, clear running, journal done. Every step tolerates having run.
finalise() {
    local health=''
    if [ "$J_kind" = install ] && [ -n "$J_project" ]; then
        health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$(cid alliance-manager)" 2>/dev/null || true)
    fi
    if ! spool_exists "result-$J_id" && ! spool_exists "done-$J_id"; then
        printf 'alliance-manager update result %s\nid=%s\nkind=%s\noutcome=%s\nfrom=%s\nto=%s\nfinished_at=%s\nmessage=%s\nhealth=%s\nlog=%s\n' \
            "$PROTOCOL" "$J_id" "$J_kind" "$J_outcome" "$J_from" "$J_to" "$(now)" \
            "$(printf '%s' "$J_message" | tr '\n' ' ' | head -c 400)" "$health" "$STATE/update-$J_id.log" \
            | spool_put "result-$J_id"
        die_at result-published
    fi
    publish_log
    refresher_stop
    retention
    spool_rm running status
    die_at running-removed
    phase 'done'
    say "request $J_id: $J_outcome — $J_message"
}

# retention — what a request leaves behind, by outcome. installed releases everything this
# request held and every earlier failed request's set. failed and rolled_back keep their set —
# the database pin, the bundle, the recovery directory, the receipt, the held images — for the
# operator when the updater got as far as a receipt (something moved), newest three, 30 days;
# with no receipt nothing moved, and the set goes. refused and ok hold nothing worth keeping.
retention() {
    local id
    case $J_outcome in
        installed)
            sets_remove "$J_id"
            while IFS= read -r id; do [ -n "$id" ] && sets_remove "$id"; done < <(cat "$STATE/kept" 2>/dev/null)
            : > "$STATE/kept"
            ;;
        failed|rolled_back)
            if [ -f "$STATE/receipt-$J_id" ]; then
                grep -qxF "$J_id" "$STATE/kept" 2>/dev/null || printf '%s\n' "$J_id" >> "$STATE/kept"
            else
                sets_remove "$J_id"
            fi
            while [ "$(grep -c . "$STATE/kept" 2>/dev/null || true)" -gt "$KEEP_SETS" ]; do
                id=$(head -n1 "$STATE/kept")
                sets_remove "$id"
                sed -i '1d' "$STATE/kept"
            done
            while IFS= read -r id; do
                [ -n "$id" ] || continue
                if [ -n "$(find "$STATE" -maxdepth 1 -name "rollback-$id.tar.gz" -mtime +"$KEEP_DAYS" 2>/dev/null)" ]; then
                    sets_remove "$id"
                    sed -i "/^$id\$/d" "$STATE/kept"
                fi
            done < <(cat "$STATE/kept" 2>/dev/null)
            ;;
        *)
            sets_remove "$J_id"
            ;;
    esac
    docker image prune -f >/dev/null 2>&1
    return 0
}

# sets_remove ID — everything request ID held: its files here, its database pin, and the
# pinned-<ID> tags on its images (which then prune as any untagged image does).
sets_remove() {
    local ref
    rm -rf -- "$STATE/rollback-$1.tar.gz" "$STATE/recovery-$1" "$STATE/receipt-$1" "$STATE/request-$1" \
        "$STATE/request-$1.sha256" "$BACKUP_DIR/pinned/$1.db"
    while IFS= read -r ref; do
        # -f: a digest-pulled image (Collabora) has no tag but this one, and Docker will not
        # remove an in-use image's last tag without it. On a tag, -f only untags.
        [ -n "$ref" ] && docker rmi -f "$ref" >/dev/null 2>&1
    done < <(docker images --format '{{.Repository}}:{{.Tag}}' 2>/dev/null | grep -- ":pinned-$1\$")
    return 0
}

# handle_request — a request is in the spool and nothing is in progress.
handle_request() {
    local body sum hdr id
    body=$(spool_read request 8192)
    case $? in
        0) ;;
        3) refuse_unclaimed "the request is larger than 8192 bytes"; return ;;
        *) refuse_unclaimed "the request is not a readable regular file"; return ;;
    esac
    hdr=$(printf '%s\n' "$body" | head -n1)
    id=$(printf '%s\n' "$body" | sed -n 's/^id=//p' | head -n1)
    if ! [[ $id =~ ^[A-Za-z0-9-]{1,40}$ ]]; then
        refuse_unclaimed "the request carries no valid id"
        return
    fi
    for k in $JOURNAL_KEYS; do printf -v "J_$k" '%s' ''; done
    J_id=$id J_kind=install J_started_at=$(now)
    rm -f "$STATE/lease" "$STATE/lease-gap" "$STATE/step"
    printf '%s' "$body" > "$STATE/request-$id"
    sum=$(printf '%s' "$body" | sha256sum | cut -d' ' -f1)
    printf '%s\n' "$sum" > "$STATE/request-$id.sha256"
    LOG="$STATE/update-$id.log"
    : > "$LOG"
    phase claiming
    spool_sh 'mv -fT -- request running' || { result refused "the request could not be claimed"; return; }
    die_at renamed
    if [ "$(printf '%s' "$(spool_read running 8192)" | sha256sum | cut -d' ' -f1)" != "$sum" ]; then
        result refused "the request changed while it was being claimed"
        return
    fi
    # The lease, at once: the app gates on running plus a fresh status naming this id.
    J_kind=install
    grep -qx 'target=noop' <<<"$body" && J_kind=noop
    J_from=$(printf '%s\n' "$body" | sed -n 's/^from=//p' | head -n1)
    phase claimed
    refresher_start

    if [ "$(printf '%s' "$hdr" | sed -n 's/^alliance-manager update request \([0-9][0-9]*\)$/\1/p')" != 1 ]; then
        if [[ $hdr =~ ^alliance-manager\ update\ request\ ([0-9]+)$ ]]; then
            result refused "the request is format ${BASH_REMATCH[1]}; this agent ($AGENT_RELEASE) reads format $PROTOCOL — update from a terminal once"
        else
            result refused "line 1 of the request is not a request header"
        fi
        return
    fi
    local n=0 targets=0 line target='' bad=''
    while IFS= read -r line; do
        n=$((n + 1))
        [ "$n" = 1 ] && continue
        case $line in
            '') ;;
            target=*)
                targets=$((targets + 1))
                target=${line#target=}
                [[ $target =~ ^(alliance-manager\ v[0-9]+\.[0-9]+\.[0-9]+|noop)$ ]] || bad="line $n names a target this agent does not know" ;;
            [a-z_]*=*) ;;
            *) bad="line $n is not key=value" ;;
        esac
        [ -n "$bad" ] && break
    done <<<"$body"
    [ "$n" -gt 64 ] && bad="the request has more than 64 lines"
    [ "$targets" -gt 8 ] && bad="the request has more than 8 targets"
    [ "$targets" = 1 ] || [ -n "$bad" ] || bad="the request must name exactly one target (it names $targets)"
    if [ -n "$bad" ]; then result refused "$bad"; return; fi

    if [ "$target" = noop ]; then
        J_kind=noop J_outcome=ok J_message="the host agent answered"
        phase verified
        result ok "the host agent ($AGENT_RELEASE) answered"
        return
    fi
    J_to=${target#alliance-manager }
    J_from=$(printf '%s\n' "$body" | sed -n 's/^from=//p' | head -n1)
    journal_save
    if [ "$J_from" != "$(env_val APP_VERSION)" ]; then
        result refused "the request is from $J_from but this install's APP_VERSION is $(env_val APP_VERSION)"
        return
    fi
    if [ -z "$(env_val HOST_FILES_VERSION)" ]; then
        result refused "this install is not on versioned host files; run ./scripts/manage.sh migrate"
        return
    fi
    if ! install_trusted forward; then
        result refused "refused: $TRUST_REASON"
        return
    fi
    J_project=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' \
        "$(cd "$APP_DIR" && docker compose ps --all --quiet alliance-manager 2>/dev/null | head -n1)" 2>/dev/null)
    if [ -z "$J_project" ]; then
        result refused "the alliance-manager container could not be found"
        return
    fi
    journal_save
    local why
    if ! why=$(mounts_ok "$(cid alliance-manager)"); then
        result refused "$why"
        return
    fi

    # The barrier: the app has stopped every writer before the backup is taken.
    step "Waiting for the app to stop taking changes"
    local waited=0
    until gate_marker gate-ack; do
        if [ "$waited" -ge "$ACK_WAIT" ]; then
            result refused "the app did not stop taking changes within ${ACK_WAIT}s (no gate-ack); nothing changed"
            return
        fi
        sleep 1
        waited=$((waited + 1))
    done
    J_ack=1
    journal_save

    prepare || return
    apply_forward
}

# refuse_unclaimed MESSAGE — a request this agent will not claim (no valid id, unreadable):
# status says why under the fixed id `invalid`, and the app removes the request.
refuse_unclaimed() {
    printf 'alliance-manager update status %s\nid=invalid\nkind=install\nstate=refused\nstep=%s\nupdated_at=%s\n' \
        "$PROTOCOL" "$1" "$(now)" | spool_put status
    loud "request refused: $1"
}

# mounts_ok CONTAINER — the running app's database and spool are on the ./data bind: for each,
# the most specific mount covering it.
mounts_ok() {
    local json p verdict
    json=$(docker inspect -f '{{json .Mounts}}' "$1" 2>/dev/null) || { echo "the app container could not be inspected"; return 1; }
    for p in /app/data/alliance.db /app/data/update; do
        verdict=$(jq -r --arg p "$p" --arg want "$APP_DIR/data" '
            map(select(.Destination as $t | $p == $t or ($p | startswith(($t | rtrimstr("/")) + "/"))))
            | sort_by(.Destination | length) | last
            | if . == null then "no mount covers \($p)"
              elif .Type == "bind" and ((.Source // "") | rtrimstr("/")) == $want then "ok"
              else "\($p) is on a \(.Type) mount of \(.Source // .Name // "?")" end' <<<"$json")
        if [ "$verdict" != ok ]; then
            echo "refused: the running app's data is not the $APP_DIR/data bind — $verdict"
            return 1
        fi
    done
}

# prepare — what recovery needs, before anything moves: the current host files as a bundle,
# every image of the current stack held by a pinned-<id> tag, the stack journaled.
prepare() {
    step "Preparing a local rollback point"
    local list svc img
    J_images_before=''
    while read -r svc img; do
        [ -n "$img" ] || continue
        if ! docker tag "$img" "$(pin_ref "$img")" >/dev/null 2>&1; then
            journal_save
            result refused "the $svc image $img is not on this host, so it could not be held for a rollback"
            return 1
        fi
        J_images_before+="$svc $img;"
    done < <(stack_images)
    if [ -z "$J_images_before" ]; then
        result refused "the current stack's images could not be read (docker compose config)"
        return 1
    fi
    list=$(mktemp)
    (cd "$APP_DIR" && while IFS= read -r p; do [ -e "$p" ] && printf '%s\n' "$p"; done < .host-files.list) > "$list"
    if ! tar -czf "$STATE/rollback-$J_id.tar.gz" -C "$APP_DIR" -T "$list"; then
        rm -f "$list"
        result refused "the rollback bundle could not be written"
        return 1
    fi
    rm -f "$list" "$STATE/receipt-$J_id"
    phase prepared
}

apply_forward() {
    step "Installing $J_to"
    phase applying
    timeout --kill-after=60 "$CHILD_TIMEOUT" env AM_ALLOW_ROOT=1 AM_NO_REMEDY=1 AM_UPDATE_LOCK_FD=9 NON_INTERACTIVE=1 \
        "$APP_DIR/scripts/manage.sh" update --version "$J_to" --yes --receipt "$STATE/receipt-$J_id" \
        >> "$LOG" 2>&1 < /dev/null
    J_child_exit=$?
    J_images_after=$(stack_images 2>/dev/null | tr '\n' ';')
    phase applied
    if [ "$J_child_exit" != 0 ]; then
        fail "the updater exited with status $J_child_exit: $(grep -v '^[[:space:]]*$' "$LOG" | tail -n1 | cut -c1-200)"
        return
    fi
    verify_forward
}

# wait_healthy CONTAINER — poll until healthy, or the container exits, or the window ends.
# A transient unhealthy is waited out: a slow first start can read unhealthy, then healthy.
wait_healthy() {
    local c=$1 waited=0 st
    while [ "$waited" -lt "$HEALTH_WAIT" ]; do
        st=$(docker inspect -f '{{.State.Running}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$c" 2>/dev/null || echo "false gone")
        case $st in
            "true healthy") return 0 ;;
            false*) HEALTH="exited ($st)"; return 1 ;;
        esac
        HEALTH=${st#* }
        sleep "$POLL"
        waited=$((waited + POLL))
    done
    return 1
}

# service_stable SERVICE — running, with the same restart count over STABLE_FOR seconds.
service_stable() {
    local c a b
    c=$(cid "$1")
    [ -n "$c" ] || return 1
    a=$(docker inspect -f '{{.State.Running}} {{.RestartCount}}' "$c" 2>/dev/null) || return 1
    sleep "$STABLE_FOR"
    b=$(docker inspect -f '{{.State.Running}} {{.RestartCount}}' "$c" 2>/dev/null) || return 1
    [ "$a" = "$b" ] && [ "${a%% *}" = true ]
}

collabora_answers() {
    local addr
    addr=$(cd "$APP_DIR" && docker compose port collabora 9980 2>/dev/null | head -n1)
    [ -n "$addr" ] || return 0      # nothing published: the stable-running check stands alone
    addr=${addr/0.0.0.0/127.0.0.1}
    curl -fsS --max-time 10 -o /dev/null "http://$addr/hosting/discovery"
}

verify_forward() {
    step "Checking $J_to is healthy"
    phase verifying
    local c img svc
    HEALTH=''
    c=$(cid alliance-manager)
    if [ -z "$c" ] || ! wait_healthy "$c"; then
        fail "the app did not become healthy within ${HEALTH_WAIT}s (${HEALTH:-not found})"
        return
    fi
    J_healthy_seen=1
    journal_save
    img=$(docker inspect -f '{{.Config.Image}}' "$c" 2>/dev/null)
    if [ "$img" != "$APP_IMAGE_REPO:$J_to" ]; then
        fail "the running image is $img, not $APP_IMAGE_REPO:$J_to (an override names another image?)"
        return
    fi
    for svc in collabora ocr-local; do
        [ "$(image_for "$J_images_before" "$svc")" = "$(image_for "$J_images_after" "$svc")" ] && continue
        [ -n "$(image_for "$J_images_after" "$svc")" ] || continue
        step "Checking $svc on its new image"
        if ! service_stable "$svc"; then
            fail "$svc did not stay up on $(image_for "$J_images_after" "$svc")"
            return
        fi
        if [ "$svc" = collabora ] && ! collabora_answers; then
            fail "Collabora does not answer its discovery URL on $(image_for "$J_images_after" collabora)"
            return
        fi
    done
    J_outcome=installed
    phase verified
    result installed "$J_to is installed and healthy"
}

# receipt_ok — the receipt apply wrote for this request: present, whole, this id.
receipt_ok() {
    local r="$STATE/receipt-$J_id"
    [ -f "$r" ] && [ "$(head -n1 "$r")" = "alliance-manager update receipt 1" ] \
        && [ "$(kv "$r" id)" = "$J_id" ] && [ "$(kv "$r" app_dir)" = "$APP_DIR" ] \
        && [ -n "$(kv "$r" phase)" ]
}

# fail REASON — something went wrong after the barrier. First: did anything move?
fail() {
    local reason=$1 r="$STATE/receipt-$J_id" rphase=''
    J_message=$reason
    journal_save
    receipt_ok && rphase=$(kv "$r" phase)
    local app_v host_v
    app_v=$(env_val APP_VERSION) host_v=$(env_val HOST_FILES_VERSION)
    if [ -z "$rphase" ] && [ "$app_v" = "$J_from" ] && [ "$host_v" = "$J_from" ] && [ ! -f "$r" ]; then
        # Stage 1 failed (download, checksum, unpack) before it touched the install.
        result failed "$reason — nothing on the host changed"
        return
    fi
    case $rphase in backed-up|armed)
        if [ -n "$J_child_exit" ] && [ "$J_child_exit" != 0 ] && [ "$app_v" = "$J_from" ] && [ "$host_v" = "$J_from" ] \
            && [ "$(docker inspect -f '{{.Config.Image}}' "$(cid alliance-manager)" 2>/dev/null)" = "$APP_IMAGE_REPO:$J_from" ]; then
            # apply's own trap put the files back; the app was never restarted.
            result failed "$reason — the updater put everything back; the app was not restarted"
            return
        fi ;;
    esac
    stop_app
}

stop_app() {
    phase stopping
    local c
    c=$(cid alliance-manager)
    if [ -n "$c" ]; then
        docker stop -t 45 "$c" >/dev/null 2>&1
        if [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null)" = true ]; then
            J_message="$J_message; the app could not be stopped, so nothing was restored"
            result failed "$J_message"
            return
        fi
    fi
    restore_db
}

# restore_db — the guarded restore. Every path to a database copy comes through here, and it
# re-checks everything each time: the app is stopped now; the receipt is this request's and its
# pinned backup matches it; the app acknowledged the barrier; the lease never lapsed and the app
# never released the gate; and a migration actually ran. If any fails, the database is left as
# it is and recovery of the files and image goes on.
restore_db() {
    phase restoring
    local r="$STATE/receipt-$J_id" pinned want live c note=''
    # A kill between the rename and the journal: the restored file is in place.
    if [ "$J_db_restored" = renaming ] && [ ! -e "$APP_DIR/data/.alliance.db.restore-$J_id" ]; then
        J_db_restored=1
        rm -f -- "$APP_DIR/data/.alliance.db-wal.aside-$J_id" "$APP_DIR/data/.alliance.db-shm.aside-$J_id"
        J_restore_note="the database was restored from the pre-update backup"
        phase restored
        recover
        return
    fi
    c=$(cid alliance-manager)
    if [ -n "$c" ] && [ "$(docker inspect -f '{{.State.Running}}' "$c" 2>/dev/null)" = true ]; then
        note="the app was running again, so the database was left alone"
    elif ! receipt_ok; then
        note="no receipt for this request, so the database was left alone"
    elif [ "$J_lease_gap" = 1 ] || gate_marker gate-released || lease_gap_seen; then
        J_lease_gap=1
        note="the app may have accepted changes after the backup (the update lease lapsed), so the database was left alone for the operator to decide"
    elif [ "$J_ack" != 1 ]; then
        note="the app never acknowledged the barrier, so the database was left alone"
    else
        pinned=$(kv "$r" pinned_db) want=$(kv "$r" schema_max)
        live=$(sqlite3 "$APP_DIR/data/alliance.db" "SELECT COALESCE(MAX(version_id), '') FROM goose_db_version WHERE is_applied = 1;" 2>/dev/null)
        chown --reference="$APP_DIR/data" "$APP_DIR"/data/alliance.db-wal "$APP_DIR"/data/alliance.db-shm 2>/dev/null
        if ! [[ $live =~ ^[0-9]+$ && $want =~ ^[0-9]+$ ]]; then
            note="the schema version could not be read, so the database was left alone (the pre-update backup is $pinned)"
        elif [ "$live" -le "$want" ]; then
            note="no migration ran, so the database needed no restore"
        elif [ ! -f "$pinned" ] || [ "$(sqlite3 "$pinned" "SELECT COALESCE(MAX(version_id), '') FROM goose_db_version WHERE is_applied = 1;" 2>/dev/null)" != "$want" ]; then
            note="the pinned backup $pinned does not match the receipt, so the database was left alone"
        else
            local d="$APP_DIR/data" tmp="$APP_DIR/data/.alliance.db.restore-$J_id"
            cp --remove-destination -- "$pinned" "$tmp" && sync -- "$tmp" || { note="the backup could not be copied back"; }
            die_at restore-copied
            if [ -z "$note" ] && [ "$(sqlite3 "$tmp" 'PRAGMA integrity_check;' 2>/dev/null)" != ok ]; then
                note="the copy of the backup failed its integrity check, so the database was left alone"
            fi
            if [ -z "$note" ] && [ "$(sqlite3 "$tmp" "SELECT COALESCE(MAX(version_id), '') FROM goose_db_version WHERE is_applied = 1;" 2>/dev/null)" != "$want" ]; then
                note="the copy of the backup is not at the receipt's schema, so the database was left alone"
            fi
            die_at restore-checked
            if [ -z "$note" ]; then
                rm -f -- "$tmp-wal" "$tmp-shm"
                chown --reference="$d" "$tmp"
                # The live database's WAL and shared-memory files are moved aside, not deleted:
                # renamed in beside a WAL from another database, SQLite would replay it into
                # the restored file. Aside, a resume that ends up not restoring can put them
                # back, so a kill between here and the rename loses nothing.
                J_db_restored=renaming
                journal_save
                wal_aside
                die_at restore-unlinked
                mv -fT -- "$tmp" "$d/alliance.db" && sync -- "$d"
                die_at restore-renamed
                J_db_restored=1
                journal_save
                rm -f -- "$d/.alliance.db-wal.aside-$J_id" "$d/.alliance.db-shm.aside-$J_id"
                note="the database was restored from the pre-update backup (schema $want)"
            else
                rm -f -- "$tmp" "$tmp-wal" "$tmp-shm"
            fi
        fi
    fi
    # Not restored after all (a resume that found the app running again, the lease lapsed…):
    # whatever was moved aside goes back.
    if [ "$J_db_restored" != 1 ]; then
        wal_back
        [ "$J_db_restored" = renaming ] && J_db_restored=''
    fi
    J_restore_note=$note
    say "$note"
    phase restored
    recover
}

wal_aside() {
    local d="$APP_DIR/data" x
    for x in wal shm; do
        [ -e "$d/alliance.db-$x" ] && mv -fT -- "$d/alliance.db-$x" "$d/.alliance.db-$x.aside-$J_id"
    done
    sync -- "$d"
}

wal_back() {
    local d="$APP_DIR/data" x
    for x in wal shm; do
        [ -e "$d/.alliance.db-$x.aside-$J_id" ] && mv -fT -- "$d/.alliance.db-$x.aside-$J_id" "$d/alliance.db-$x"
    done
    rm -f -- "$d/.alliance.db.restore-$J_id"
    return 0
}

# recover — the previous release's own apply, from the bundle, offline: never the install's
# copy, which an interrupted update may have left half-replaced.
recover() {
    local rec="$STATE/recovery-$J_id"
    step "Returning to $J_from"
    phase rolling-back
    rm -rf -- "$rec"
    mkdir -p "$rec"
    if ! tar -xzf "$STATE/rollback-$J_id.tar.gz" -C "$rec" || [ ! -f "$rec/scripts/manage.sh" ]; then
        recovery_failed "the rollback bundle could not be unpacked"
        return
    fi
    timeout --kill-after=60 "$CHILD_TIMEOUT" env AM_ALLOW_ROOT=1 AM_NO_REMEDY=1 AM_UPDATE_LOCK_FD=9 NON_INTERACTIVE=1 \
        bash "$rec/scripts/manage.sh" apply --app-dir "$APP_DIR" --target "$J_from" --staging "$rec" --offline --yes \
        >> "$LOG" 2>&1 < /dev/null
    J_child_exit=$?
    phase rolled-back
    verify_recovered
}

verify_recovered() {
    if [ "$J_child_exit" != 0 ]; then
        recovery_failed "the return to $J_from failed (status $J_child_exit)"
        return
    fi
    local c
    HEALTH=''
    c=$(cid alliance-manager)
    if [ -n "$c" ] && wait_healthy "$c" \
        && [ "$(docker inspect -f '{{.Config.Image}}' "$c" 2>/dev/null)" = "$APP_IMAGE_REPO:$J_from" ]; then
        result rolled_back "$J_message; returned to $J_from, which is healthy. $J_restore_note"
    else
        result failed "$J_message; returned to $J_from, but it is not healthy (${HEALTH:-not found}). $J_restore_note"
    fi
}

# recovery_failed REASON — no clean way back. With the database untouched, the install is
# brought up on whatever release its files now name, so it runs; with the database restored to
# the previous release's schema, starting the newer binary on it is the one thing not to do: the
# app stays stopped and the operator is told, every tick, until they act.
recovery_failed() {
    if [ "$J_db_restored" = 1 ]; then
        J_message="$J_message; $1. The database was restored to the pre-update backup, so the app was left STOPPED — see docs/DEPLOYMENT.md → Recovering from a failed update"
        J_outcome=failed
        journal_save
        finalise_needs_operator
        return
    fi
    (cd "$APP_DIR" && docker compose up -d --pull never) >> "$LOG" 2>&1
    result failed "$J_message; $1. The app was started on its current files; check it with ./scripts/manage.sh status"
}

finalise_needs_operator() {
    if ! spool_exists "result-$J_id" && ! spool_exists "done-$J_id"; then
        printf 'alliance-manager update result %s\nid=%s\nkind=%s\noutcome=failed\nfrom=%s\nto=%s\nfinished_at=%s\nmessage=%s\nhealth=stopped\nlog=%s\n' \
            "$PROTOCOL" "$J_id" "$J_kind" "$J_from" "$J_to" "$(now)" "$(printf '%s' "$J_message" | tr '\n' ' ' | head -c 400)" \
            "$STATE/update-$J_id.log" | spool_put "result-$J_id"
    fi
    publish_log
    refresher_stop
    spool_rm running status
    phase needs-operator
    loud "request $J_id needs the operator: $J_message"
}

# resume — a journal not at done: carry it to a terminal outcome from what is true now.
resume() {
    LOG="$STATE/update-$J_id.log"
    case $J_phase in
        done) return 0 ;;
        needs-operator) loud "request $J_id still needs the operator: $J_message"; return 0 ;;
    esac
    # The lapse is judged before the lease is renewed: the gap up to now counts.
    lease_gap_seen && J_lease_gap=1
    if [ -n "$J_format" ] && [ "$J_format" -gt "$PROTOCOL" ]; then
        loud "the journal is format $J_format, newer than this agent reads ($PROTOCOL); leaving it for the operator"
        return 0
    fi
    say "resuming request $J_id from $J_phase"
    case $J_phase in
        claiming)
            if spool_present request && ! spool_exists running; then
                # Killed before the rename: nothing was claimed, so the request is simply handled.
                say "the previous tick stopped before claiming $J_id; handling it now"
                handle_request
                return
            fi
            refresher_start
            result refused "interrupted before it was claimed" ;;
        claimed|prepared)
            refresher_start
            result failed "interrupted before anything changed" ;;
        applying)
            refresher_start
            J_images_after=$(stack_images 2>/dev/null | tr '\n' ';')
            local app_v host_v
            app_v=$(env_val APP_VERSION) host_v=$(env_val HOST_FILES_VERSION)
            if [ "$app_v" = "$J_to" ] && [ "$host_v" = "$J_to" ] && receipt_ok; then
                verify_forward
            elif [ "$app_v" = "$J_from" ] && [ "$host_v" = "$J_from" ] && [ ! -f "$STATE/receipt-$J_id" ]; then
                result failed "interrupted before the updater changed anything — nothing on the host changed"
            else
                J_message="interrupted while the updater ran"
                journal_save
                stop_app
            fi ;;
        applied)
            refresher_start
            if [ "$J_child_exit" = 0 ]; then verify_forward; else fail "the updater exited with status $J_child_exit"; fi ;;
        verifying)
            refresher_start
            verify_forward ;;
        verified)
            refresher_start
            if [ "$J_kind" = noop ]; then
                result ok "the host agent ($AGENT_RELEASE) answered"
            else
                result installed "$J_to is installed and healthy"
            fi ;;
        stopping)
            refresher_start
            stop_app ;;
        restoring)
            refresher_start
            restore_db ;;
        restored)
            refresher_start
            recover ;;
        rolling-back)
            refresher_start
            recover ;;
        rolled-back)
            refresher_start
            verify_recovered ;;
        finalising)
            refresher_start
            finalise ;;
        *)
            loud "the journal names an unknown phase '$J_phase'; leaving it for the operator" ;;
    esac
}

# ── One install, one tick ───────────────────────────────────────────────────────────────────

install_tick() {
    NAME=$1 APP_DIR=$2 STATE="$STATE_DIR/$1" JOURNAL="$STATE_DIR/$1/journal" LOG='' REFRESHER=''
    install -d -m 0700 "$STATE"
    spool_open
    case $? in
        0) ;;
        2) return 0 ;;
        *)
            # Said once per reason, not every 30 seconds.
            if [ "$(cat "$STATE/skipped" 2>/dev/null)" != "$TRUST_REASON" ]; then
                loud "skipped: $TRUST_REASON"
                printf '%s' "$TRUST_REASON" > "$STATE/skipped"
            fi
            return 0 ;;
    esac
    rm -f "$STATE/skipped"
    owner_record
    journal_load
    local busy=0
    if [ -n "$J_phase" ] && [ "$J_phase" != 'done' ] && [ "$J_phase" != needs-operator ]; then
        busy=1
    elif spool_present request || spool_exists running; then
        busy=1
    fi
    if [ "$busy" = 1 ]; then
        if [ ! -e "$APP_DIR/.update.lock" ]; then
            : >> "$APP_DIR/.update.lock" && chown --reference="$APP_DIR" "$APP_DIR/.update.lock"
        fi
        exec 9>>"$APP_DIR/.update.lock"
        if ! flock -n 9; then
            say "an update started from a terminal is running; the request waits"
            exec 9>&-
            spool_close
            return 0
        fi
        if [ -n "$J_phase" ] && [ "$J_phase" != 'done' ] && [ "$J_phase" != needs-operator ]; then
            resume
        elif spool_exists running; then
            # running with no journal for it: killed between the rename and the first journal
            # write of a claim this agent never recorded. Nothing in that state moved anything.
            J_id=$(spool_read running 8192 | sed -n 's/^id=//p' | head -n1)
            [[ $J_id =~ ^[A-Za-z0-9-]{1,40}$ ]] || J_id=invalid
            J_kind=install J_phase=claiming
            LOG="$STATE/update-$J_id.log"
            refresher_start
            result refused "interrupted before it was claimed"
        elif [ "$J_phase" = needs-operator ]; then
            loud "request $J_id still needs the operator: $J_message"
        else
            handle_request
        fi
        refresher_stop
        exec 9>&-
    elif [ "$J_phase" = needs-operator ]; then
        loud "request $J_id still needs the operator: $J_message"
    fi
    spool_close
}

# registry — "name dir" per registered install (registry.sh's format, read, not sourced).
registry() {
    local f
    for f in "$REGISTRY_DIR"/*.conf; do
        [ -e "$f" ] || continue
        printf '%s %s\n' "$(basename "$f" .conf)" "$(sed -n 's/^APP_DIR="\(.*\)"$/\1/p' "$f" | head -n1)"
    done
}

on_exit() {
    refresher_stop
}

main() {
    trap on_exit EXIT
    trap 'exit 143' TERM
    trap 'exit 130' INT
    exec {TICK}>>"$TICK_LOCK" || exit 1
    flock -n "$TICK" || exit 0
    command -v jq >/dev/null 2>&1 || { loud "jq is not installed; the update agent cannot read compose's configuration (sudo apt-get install jq)"; exit 1; }
    local name dir entries
    entries=$(registry)
    # Every install's heartbeat first, so one long update never reads as a dead agent on another.
    while read -r name dir; do
        [ -n "$dir" ] || continue
        NAME=$name APP_DIR=$dir
        if spool_open; then heartbeat; fi
        spool_close
    done <<<"$entries"
    while read -r name dir; do
        [ -n "$dir" ] || continue
        install_tick "$name" "$dir"
    done <<<"$entries"
    NAME=''
}

main "$@"
