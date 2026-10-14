# shellcheck shell=bash
# scripts/lib/common.sh — shared helpers for install.sh and manage.sh.
#
# Sourced, never executed: this file defines functions and variables and has no other effect
# at source time, so tests can source it and call any function in isolation.
#
# FROZEN SURFACE: manage.sh's stage 1 is always the PREVIOUS release's copy, and it calls
# resolve_latest_release, download_asset and unpack_asset from the lib/ beside it. A change to
# what those three take or return reaches an install one release late. Add, don't change.

# --- System locations ---------------------------------------------------------------------
# Overridable so the tests can point them at a temp directory; nothing in production sets
# them. The backup helper written to AM_BACKUP_HELPER gets these paths baked in as literals.
AM_REPO=${AM_REPO:-shodiwarmic/lastwar-alliance-manager}
AM_REGISTRY_DIR=${AM_REGISTRY_DIR:-/etc/alliance-manager/installs.d}
AM_BACKUP_DIR=${AM_BACKUP_DIR:-/var/backups/lastwar}
AM_BACKUP_HELPER=${AM_BACKUP_HELPER:-/usr/local/bin/backup-lastwar.sh}
AM_LOG_DIR=${AM_LOG_DIR:-/var/log/lastwar}
AM_CADDYFILE=${AM_CADDYFILE:-/etc/caddy/Caddyfile}
# Root-owned state of the host update agent, one directory per registered install: the
# journal, the claimed request's copy, receipts and the rollback bundle. The app never sees it.
AM_STATE_DIR=${AM_STATE_DIR:-/var/lib/alliance-manager}

# The first release that ships a host-files asset. Anything older can only be reached by
# the manual pin path (set APP_VERSION, compose pull).
# shellcheck disable=SC2034  # read by manage.sh
AM_FIRST_ASSET_RELEASE=v2.0.0

# --- Output -------------------------------------------------------------------------------
# shellcheck disable=SC2034  # the full palette, for the scripts that source this file
if [ -t 1 ]; then
    C_RED=$'\033[0;31m' C_GREEN=$'\033[0;32m' C_YELLOW=$'\033[1;33m' C_BOLD=$'\033[1m' C_NC=$'\033[0m'
else
    C_RED='' C_GREEN='' C_YELLOW='' C_BOLD='' C_NC=''
fi

log()  { printf '%s\n' "$*"; }
step() { printf '%s==> %s%s\n' "$C_GREEN" "$*" "$C_NC"; }
warn() { printf '%sWarning:%s %s\n' "$C_YELLOW" "$C_NC" "$*" >&2; }
die()  { printf '%sError:%s %s\n' "$C_RED" "$C_NC" "$*" >&2; exit 1; }

# confirm "question" — yes/no prompt. NON_INTERACTIVE=1 answers no without asking, so a
# script that needs a yes in automation must take a flag for it rather than hang.
confirm() {
    local reply
    if [ "${NON_INTERACTIVE:-0}" = 1 ] || [ ! -t 0 ]; then
        return 1
    fi
    read -r -p "$1 [y/N]: " reply
    [[ $reply =~ ^[Yy]$ ]]
}

# --- Packages -----------------------------------------------------------------------------
# DEBIAN_FRONTEND=noninteractive: on a terminal, a package's debconf questions (exim's mail
# setup, pulled in by fail2ban's recommends, is one) would otherwise stop the install to ask.
apt_update()  { sudo DEBIAN_FRONTEND=noninteractive apt-get update; }
apt_install() { sudo DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"; }

# --- .env ---------------------------------------------------------------------------------
# .env is read with grep, NEVER `source`d: the documented TRUSTED_ORIGINS is an unquoted
# value with spaces (app.example.com, localhost:8080, …), which a shell parses as a command
# and, under `set -e`, dies on. The old update.sh sourced it, and only got away with it on
# installs that never set that line.

# env_file — the .env these helpers act on: $ENV_FILE, else $APP_DIR/.env.
env_file() { printf '%s' "${ENV_FILE:-$APP_DIR/.env}"; }

# env_has KEY — true if KEY has an active (uncommented) line, even an empty one.
env_has() {
    grep -q "^$1=" "$(env_file)" 2>/dev/null
}

# env_get KEY — the value of KEY's first active line, one pair of surrounding quotes removed.
# Prints nothing (and succeeds) when the key is absent.
env_get() {
    local line
    line=$(grep -m1 "^$1=" "$(env_file)" 2>/dev/null) || return 0
    line=${line#*=}
    if [[ $line == \"*\" && ${#line} -ge 2 ]]; then
        line=${line:1:${#line}-2}
    elif [[ $line == \'*\' && ${#line} -ge 2 ]]; then
        line=${line:1:${#line}-2}
    fi
    printf '%s' "$line"
}

# env_set KEY VALUE — replace KEY's active line in place, or append one. A commented
# `# KEY=…` line (as .env.example carries) is documentation and is left alone. The value is
# passed through the environment rather than awk -v, which would interpret backslashes.
env_set() {
    local key=$1 value=$2 file tmp
    file=$(env_file)
    if env_has "$key"; then
        tmp=$(mktemp "$file.XXXXXX")
        K="$key" V="$value" awk '
            !done && index($0, ENVIRON["K"] "=") == 1 { print ENVIRON["K"] "=" ENVIRON["V"]; done = 1; next }
            { print }' "$file" > "$tmp"
        chmod --reference="$file" "$tmp"
        mv "$tmp" "$file"
    else
        # .env.example ends without a newline; appending to that would glue two lines.
        if [ -s "$file" ] && [ -n "$(tail -c1 "$file")" ]; then
            printf '\n' >> "$file"
        fi
        printf '%s=%s\n' "$key" "$value" >> "$file"
    fi
}

# env_ensure KEY VALUE — set KEY only if it has no active line. What a re-run uses, so it
# adds what is missing and changes nothing an operator has set.
env_ensure() {
    env_has "$1" || env_set "$1" "$2"
}

# --- Releases -----------------------------------------------------------------------------

# is_release_tag TAG — vX.Y.Z, the only shape a release is tagged with.
is_release_tag() {
    [[ $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

# version_lt A B — true if release tag A sorts before B (both vX.Y.Z).
version_lt() {
    [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$1" ]
}

# resolve_latest_release — print the newest published release's tag, or nothing.
# Never fails: no curl, no network and "no release yet" (the API's 404) all print nothing,
# and the caller keeps whatever pin it had. jq is not needed here: one known key in a known
# payload is a sed away. (apply's data-mount check does need it, for compose's JSON.)
# [frozen — see the header]
resolve_latest_release() {
    local json
    command -v curl >/dev/null 2>&1 || return 0
    json=$(curl -fsS --max-time 20 "https://api.github.com/repos/$AM_REPO/releases/latest" 2>/dev/null) || return 0
    printf '%s' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
}

# download_asset TAG DIR — fetch host-files.tar.gz and its .sha256 for TAG into DIR and
# verify one against the other. The checksum is transport integrity, not authenticity: it
# comes from the same origin over the same TLS, which is the trust `git pull` had.
# [frozen — see the header]
download_asset() {
    local tag=$1 dir=$2 base
    base="https://github.com/$AM_REPO/releases/download/$tag"
    curl -fsSL --max-time 300 -o "$dir/host-files.tar.gz" "$base/host-files.tar.gz" || return 1
    curl -fsSL --max-time 60 -o "$dir/host-files.tar.gz.sha256" "$base/host-files.tar.gz.sha256" || return 1
    (cd "$dir" && sha256sum -c --status host-files.tar.gz.sha256) || {
        warn "host-files.tar.gz for $tag does not match its published checksum"
        return 1
    }
}

# unpack_asset FILE DIR — extract a host-files tarball into DIR (created if needed) and check
# it is one: it must carry HOST_FILES_VERSION and scripts/manage.sh. [frozen — see the header]
unpack_asset() {
    local file=$1 dir=$2
    mkdir -p "$dir"
    tar -xzf "$file" -C "$dir" || return 1
    if [ ! -f "$dir/HOST_FILES_VERSION" ] || [ ! -f "$dir/scripts/manage.sh" ]; then
        warn "$file is not a host-files asset (no HOST_FILES_VERSION or scripts/manage.sh)"
        return 1
    fi
}

# --- Caddy --------------------------------------------------------------------------------
# deploy/Caddyfile is the one template. Its first line carries a revision; bump it whenever
# the template changes, and every install re-renders on its next update. A rendered file
# with no marker (every install from before v2.0.0) reads as revision 0.

# caddyfile_rev FILE — the revision on FILE's first line, 0 if unmarked or absent.
caddyfile_rev() {
    local rev=''
    if [ -r "$1" ]; then
        rev=$(sed -n '1s/^# alliance-manager caddyfile rev \([0-9][0-9]*\).*/\1/p' "$1")
    fi
    printf '%s' "${rev:-0}"
}

# valid_domain VALUE — a host name, optionally with a port. Checked before a value is
# rendered into the Caddyfile, and so before anything can be injected into it.
valid_domain() {
    [[ $1 =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]+)?$ ]]
}

# placeholder_domain VALUE — empty, or one of .env.example's example.com placeholders.
placeholder_domain() {
    [ -z "$1" ] || [[ $1 == *example.com ]]
}

# render_caddyfile TEMPLATE APP_DOMAIN COLLABORA_DOMAIN — print the rendered file.
render_caddyfile() {
    sed -e "s|__APP_DOMAIN__|$2|g" -e "s|__COLLABORA_DOMAIN__|$3|g" "$1"
}

# --- Nightly backup helper ----------------------------------------------------------------

# backup_helper_text REGISTRY_DIR BACKUP_DIR — the text of /usr/local/bin/backup-lastwar.sh,
# with both paths baked in as literals. One copy, so install.sh and manage.sh migrate write
# the same bytes.
#
# The helper runs from ROOT's cron, so it is root-owned and self-contained: it executes
# nothing from an install directory (which the operator can write — cron running
# $APP_DIR/scripts/manage.sh would make that file scheduled root code), sources nothing but
# the root-owned registry, and runs sqlite3 itself. It iterates every registered install,
# which is why the install's name is in each backup's filename.
backup_helper_text() {
    cat <<EOF
#!/bin/bash
# Nightly database backup for every registered Alliance Manager install.
# Written by scripts/install.sh / scripts/manage.sh migrate — edits are overwritten.
# Root-owned and self-contained on purpose: see scripts/lib/common.sh (backup_helper_text).
set -e
# Database copies hold every member's data: nothing this writes is for other local users.
umask 077
REGISTRY_DIR='$1'
BACKUP_DIR='$2'
mkdir -p "\$BACKUP_DIR"
for f in "\$REGISTRY_DIR"/*.conf; do
    # An unmatched glob stays the literal pattern; this is the empty-registry exit.
    if [ ! -e "\$f" ]; then
        echo "no registered installs in \$REGISTRY_DIR" >&2
        exit 1
    fi
    name=\$(basename "\$f" .conf)
    APP_DIR=
    . "\$f"
    db="\$APP_DIR/data/alliance.db"
    if [ ! -f "\$db" ]; then
        echo "\$name: no database at \$db" >&2
        continue
    fi
    # An update holds this lock from its first step to its last; a backup taken while one
    # runs could copy a database half way through a migration or a restore. Skipped, not
    # waited for: tomorrow night's run will take it. The lock is the install owner's file, so
    # a run that has to create it hands it over.
    lock="\$APP_DIR/.update.lock"
    if [ ! -e "\$lock" ]; then
        : >> "\$lock" && chown --reference="\$APP_DIR" "\$lock"
    fi
    ts=\$(date +%Y%m%d_%H%M%S)
    # 75 is flock's answer for "held"; anything else non-zero is the backup's own failure.
    rc=0
    flock -n -E 75 "\$lock" sqlite3 "\$db" ".backup '\$BACKUP_DIR/nightly_\${name}_\$ts.db'" || rc=\$?
    if [ "\$rc" = 75 ]; then
        echo "\$name: an update of this install is running (\$lock is held) — skipped tonight" >&2
        continue
    fi
    [ "\$rc" = 0 ] || exit "\$rc"
    # Opening the database can create -wal/-shm as root while the container is down; hand
    # them back to the database's owner.
    for x in "\$db-wal" "\$db-shm"; do
        if [ -e "\$x" ]; then
            chown --reference="\$db" "\$x"
        fi
    done
    echo "\$name: backed up to \$BACKUP_DIR/nightly_\${name}_\$ts.db"
done
# nightly_ is this helper's pool; alliance_ is the pool the pre-v2.0.0 helper wrote, under the
# same seven-day rule. The pre-update db_/app_ backups are manage.sh's, kept newest-ten.
find "\$BACKUP_DIR" -maxdepth 1 \( -name 'nightly_*.db' -o -name 'alliance_*.db' \) -mtime +7 -delete
EOF
}

# ensure_backup_helper [--text-only] — install (or replace) the helper and root's crontab line.
# Prints one line saying what it did. An existing helper that is neither ours nor the
# pre-v2.0.0 shape is kept aside, dated, rather than silently overwritten. --text-only (every
# update's refresh) rewrites the helper and leaves root's crontab alone.
ensure_backup_helper() {
    local tmp current outcome=written text_only=${1:-}
    tmp=$(mktemp)
    backup_helper_text "$AM_REGISTRY_DIR" "$AM_BACKUP_DIR" > "$tmp"
    if [ -e "$AM_BACKUP_HELPER" ]; then
        current=$(cat "$AM_BACKUP_HELPER")
        if [ "$current" = "$(cat "$tmp")" ]; then
            outcome=unchanged
        elif grep -q 'Nightly database backup for every registered Alliance Manager install' <<<"$current" \
            || grep -q '^DB_PATH=' <<<"$current"; then
            outcome=replaced
        else
            sudo cp -p "$AM_BACKUP_HELPER" "$AM_BACKUP_HELPER.backup_$(date +%Y%m%d_%H%M%S)"
            outcome="replaced (the previous file was not ours; kept as $AM_BACKUP_HELPER.backup_*)"
        fi
    fi
    # Each step checked explicitly, so a failure is reported even where a caller has turned
    # `set -e` off.
    if [ "$outcome" != unchanged ]; then
        # Run as root, install leaves the file root-owned — which is the point.
        sudo install -D -m 0755 "$tmp" "$AM_BACKUP_HELPER" || { rm -f "$tmp"; warn "could not write $AM_BACKUP_HELPER"; return 1; }
    fi
    rm -f "$tmp"

    if [ "$text_only" = --text-only ]; then
        log "Nightly backup helper: $outcome"
        return 0
    fi
    sudo mkdir -p "$AM_LOG_DIR" || return 1
    if ! sudo crontab -l 2>/dev/null | grep -qF "$AM_BACKUP_HELPER"; then
        { sudo crontab -l 2>/dev/null || true
          printf '0 2 * * * %s >> %s/backup.log 2>&1\n' "$AM_BACKUP_HELPER" "$AM_LOG_DIR"
        } | sudo crontab - || { warn "could not add the backup helper to root's crontab"; return 1; }
        outcome="$outcome; crontab line added"
    fi
    log "Nightly backup helper: $outcome"
}

# --- Host update agent --------------------------------------------------------------------
# The root-run agent that installs a release an administrator chose on the Admin page. Its
# text is lib/update-agent.sh with this host's paths and the release baked in; it is written
# root-owned to AM_UPDATE_AGENT and run by a systemd timer every 30 seconds. Like the backup
# helper it executes nothing from an install directory except, for the forward update, the
# install's own manage.sh after checking the chain it sources — see the header of
# lib/update-agent.sh for its trust rules and its state machine.
AM_UPDATE_AGENT=${AM_UPDATE_AGENT:-/usr/local/bin/alliance-manager-update.sh}
AM_UNIT_DIR=${AM_UNIT_DIR:-/etc/systemd/system}
AM_UPDATE_UNIT=alliance-manager-update

# update_agent_text RELEASE — the agent as installed: the template with its @...@ values filled.
update_agent_text() {
    local t
    t=$(cat "$(dirname "${BASH_SOURCE[0]}")/update-agent.sh") || return 1
    t=${t//@AGENT_RELEASE@/$1}
    t=${t//@REGISTRY_DIR@/$AM_REGISTRY_DIR}
    t=${t//@BACKUP_DIR@/$AM_BACKUP_DIR}
    t=${t//@STATE_DIR@/$AM_STATE_DIR}
    t=${t//@SPOOL_UID@/1001}
    printf '%s\n' "$t"
}

# update_unit_text service|timer — the two systemd units.
update_unit_text() {
    case $1 in
        service) cat <<EOT
# Written by scripts/install.sh / scripts/manage.sh — edits are overwritten on the next update.
[Unit]
Description=Alliance Manager host update agent (installs updates requested on the Admin page)
Documentation=https://github.com/$AM_REPO/blob/main/docs/DEPLOYMENT.md
After=docker.service
Wants=docker.service

[Service]
Type=oneshot
ExecStart=$AM_UPDATE_AGENT
# Two 30-minute updater runs (the update, then a rollback), two 5-minute health waits, and
# the preparation around them.
TimeoutStartSec=90min
# An idle tick says nothing; progress is notice, problems warning.
LogLevelMax=notice
Environment=HOME=/root
EOT
        ;;
        timer) cat <<EOT
# Written by scripts/install.sh / scripts/manage.sh — edits are overwritten on the next update.
# To stop the agent: sudo systemctl disable --now $AM_UPDATE_UNIT.timer (updates keep that choice).
[Unit]
Description=Run the Alliance Manager host update agent every 30 seconds

[Timer]
OnBootSec=1min
OnUnitActiveSec=30s
AccuracySec=5s
Persistent=false

[Install]
WantedBy=timers.target
EOT
        ;;
    esac
}

# ensure_update_agent RELEASE — install or replace the agent and its two units. Prints one line
# saying what it did. A replaced file's previous bytes are kept in AGENT_SNAPSHOT (set here) so
# a failed apply can put them back (restore_update_agent); CREATED_AGENT=1 when there was no
# agent before. A new install's timer is enabled and started; a replaced one keeps whatever
# state the operator left it in.
ensure_update_agent() {
    local tmp f outcome=unchanged existed=0 changed=0 enabled=0
    tmp=$(mktemp -d)
    update_agent_text "$1" > "$tmp/agent" || { rm -rf "$tmp"; warn "could not build the update agent"; return 1; }
    update_unit_text service > "$tmp/$AM_UPDATE_UNIT.service"
    update_unit_text timer > "$tmp/$AM_UPDATE_UNIT.timer"
    [ -e "$AM_UPDATE_AGENT" ] && existed=1
    AGENT_SNAPSHOT=$(mktemp -d)
    for f in "$AM_UPDATE_AGENT:agent" "$AM_UNIT_DIR/$AM_UPDATE_UNIT.service:$AM_UPDATE_UNIT.service" \
        "$AM_UNIT_DIR/$AM_UPDATE_UNIT.timer:$AM_UPDATE_UNIT.timer"; do
        if [ -e "${f%%:*}" ]; then
            cmp -s "${f%%:*}" "$tmp/${f##*:}" && continue
            sudo cp -p "${f%%:*}" "$AGENT_SNAPSHOT/${f##*:}"
        fi
        changed=1
    done
    if [ "$changed" = 0 ]; then
        rm -rf "$tmp" "$AGENT_SNAPSHOT"
        AGENT_SNAPSHOT=''
        log "Host update agent: unchanged"
        return 0
    fi
    sudo systemctl is-enabled --quiet "$AM_UPDATE_UNIT.timer" 2>/dev/null && enabled=1
    printf '%s\n' "$enabled" > "$AGENT_SNAPSHOT/enabled"
    [ "$existed" = 0 ] && CREATED_AGENT=1
    if ! sudo install -D -m 0755 "$tmp/agent" "$AM_UPDATE_AGENT" \
        || ! sudo install -D -m 0644 "$tmp/$AM_UPDATE_UNIT.service" "$AM_UNIT_DIR/$AM_UPDATE_UNIT.service" \
        || ! sudo install -D -m 0644 "$tmp/$AM_UPDATE_UNIT.timer" "$AM_UNIT_DIR/$AM_UPDATE_UNIT.timer"; then
        rm -rf "$tmp"
        warn "could not write the host update agent"
        return 1
    fi
    rm -rf "$tmp"
    sudo systemctl daemon-reload || { warn "systemctl daemon-reload failed"; return 1; }
    if [ "$existed" = 0 ]; then
        sudo systemctl enable --now "$AM_UPDATE_UNIT.timer" || { warn "could not enable $AM_UPDATE_UNIT.timer"; return 1; }
        outcome="written; $AM_UPDATE_UNIT.timer enabled"
    else
        outcome=replaced
    fi
    log "Host update agent: $outcome"
}

# restore_update_agent — undo ensure_update_agent after a failed apply: the previous files back,
# or, where there were none, the new ones removed and the timer stopped.
restore_update_agent() {
    local f
    [ -n "${AGENT_SNAPSHOT:-}" ] && [ -d "$AGENT_SNAPSHOT" ] || return 0
    if [ "${CREATED_AGENT:-0}" = 1 ]; then
        sudo systemctl disable --now "$AM_UPDATE_UNIT.timer" 2>/dev/null
        sudo rm -f "$AM_UPDATE_AGENT" "$AM_UNIT_DIR/$AM_UPDATE_UNIT.service" "$AM_UNIT_DIR/$AM_UPDATE_UNIT.timer"
    else
        [ -f "$AGENT_SNAPSHOT/agent" ] && sudo install -m 0755 "$AGENT_SNAPSHOT/agent" "$AM_UPDATE_AGENT"
        for f in service timer; do
            [ -f "$AGENT_SNAPSHOT/$AM_UPDATE_UNIT.$f" ] && sudo install -m 0644 "$AGENT_SNAPSHOT/$AM_UPDATE_UNIT.$f" "$AM_UNIT_DIR/$AM_UPDATE_UNIT.$f"
        done
    fi
    sudo systemctl daemon-reload 2>/dev/null
    rm -rf "$AGENT_SNAPSHOT"
    AGENT_SNAPSHOT=''
    return 0
}
