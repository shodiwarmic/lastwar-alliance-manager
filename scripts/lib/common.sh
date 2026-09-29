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
# and the caller keeps whatever pin it had. jq is deliberately not a dependency: one known
# key in a known payload is a sed away. [frozen — see the header]
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
    ts=\$(date +%Y%m%d_%H%M%S)
    sqlite3 "\$db" ".backup '\$BACKUP_DIR/nightly_\${name}_\$ts.db'"
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

# ensure_backup_helper — install (or replace) the helper and root's crontab line. Prints one
# line saying what it did. An existing helper that is neither ours nor the pre-v2.0.0 shape
# is kept aside, dated, rather than silently overwritten.
ensure_backup_helper() {
    local tmp current outcome=written
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
    if [ "$outcome" != unchanged ]; then
        # Run as root, install leaves the file root-owned — which is the point.
        sudo install -D -m 0755 "$tmp" "$AM_BACKUP_HELPER"
    fi
    rm -f "$tmp"

    sudo mkdir -p "$AM_LOG_DIR"
    if ! sudo crontab -l 2>/dev/null | grep -qF "$AM_BACKUP_HELPER"; then
        { sudo crontab -l 2>/dev/null || true
          printf '0 2 * * * %s >> %s/backup.log 2>&1\n' "$AM_BACKUP_HELPER" "$AM_LOG_DIR"
        } | sudo crontab -
        outcome="$outcome; crontab line added"
    fi
    log "Nightly backup helper: $outcome"
}
