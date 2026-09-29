# shellcheck shell=bash
# scripts/lib/registry.sh — where installs are recorded, so a host knows what it runs.
#
# Sourced, never executed; functions only. Needs common.sh sourced first.
#
# FROZEN FORMAT. One file per install, /etc/alliance-manager/installs.d/<name>.conf, root-owned,
# holding exactly one active line:
#
#     APP_DIR="/path/to/install"
#
# Only APP_DIR: everything else about an install (version, OCR mode, domains) is already in
# its .env, and a second copy here would drift. The first install is named `default`. The
# nightly backup helper sources these files as root, which is why they are written only
# through `sudo install` and why registry_write refuses a directory that could not be sourced
# safely. Adding a key later is additive; changing this one is a format break that a later
# release's tooling (and the root-owned helper already on disk) would have to live with.

# registry_dir — the directory holding the <name>.conf files.
registry_dir() { printf '%s' "$AM_REGISTRY_DIR"; }

# registry_list — print each registered install's name, one per line, sorted.
registry_list() {
    local f
    for f in "$(registry_dir)"/*.conf; do
        [ -e "$f" ] || continue
        basename "$f" .conf
    done
}

# registry_get NAME — print the APP_DIR recorded for NAME (read, not sourced).
registry_get() {
    local f
    f="$(registry_dir)/$1.conf"
    [ -r "$f" ] || return 1
    sed -n 's/^APP_DIR="\(.*\)"$/\1/p' "$f" | head -n1
}

# registry_name_for DIR — print the name registered for DIR, if any.
registry_name_for() {
    local name
    while IFS= read -r name; do
        if [ "$(registry_get "$name")" = "$1" ]; then
            printf '%s' "$name"
            return 0
        fi
    done < <(registry_list)
    return 1
}

# registry_valid_name NAME — a name usable as a filename and in a backup's filename.
registry_valid_name() {
    [[ $1 =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]
}

# registry_write NAME DIR [--replace] — record DIR under NAME. Writing the same DIR again is a
# no-op, so a re-run is safe; a DIFFERENT DIR under an existing name is refused unless
# --replace, because the likeliest cause is an install that was moved or a second install
# reusing a name, and silently repointing it would retarget the nightly backup.
registry_write() {
    local name=$1 dir=$2 replace=${3:-} file existing tmp
    registry_valid_name "$name" || { warn "invalid install name '$name' (letters, digits, . _ - only)"; return 1; }
    case $dir in
        /*) ;;
        *) warn "install directory must be an absolute path: $dir"; return 1 ;;
    esac
    # The file is sourced as root by the backup helper: nothing in the value may be live.
    if [[ $dir == *[\"\$\`\\]* || $dir == *$'\n'* ]]; then
        warn "install directory contains a character the registry cannot record safely: $dir"
        return 1
    fi
    file="$(registry_dir)/$name.conf"
    if [ -e "$file" ]; then
        existing=$(registry_get "$name")
        if [ "$existing" = "$dir" ]; then
            return 0
        fi
        if [ "$replace" != --replace ]; then
            warn "an install named '$name' is already registered at $existing ($file)."
            warn "If that install was moved here, edit APP_DIR in $file; if it was removed, delete $file."
            return 1
        fi
    fi
    tmp=$(mktemp)
    printf '# Alliance Manager install "%s" — written by %s on %s\nAPP_DIR="%s"\n' \
        "$name" "${AM_WRITER:-${0##*/}}" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$dir" > "$tmp"
    # Checked explicitly: callers use `registry_write … || …`, where `set -e` does not apply.
    if ! sudo install -d -m 0755 "$(registry_dir)" || ! sudo install -m 0644 "$tmp" "$file"; then
        rm -f "$tmp"
        warn "could not write $file"
        return 1
    fi
    rm -f "$tmp"
}

# registry_remove NAME
registry_remove() {
    sudo rm -f "$(registry_dir)/$1.conf"
}

# registry_resolve [NAME] — print the install directory to act on, or explain on stderr and
# return 1. No entry: point at both ways of making one. One entry and no NAME: that one.
# Several and no NAME: list them. A directory that no longer holds docker-compose.yml was
# almost certainly moved, and the fix is an edit to the file named.
registry_resolve() {
    local want=${1:-} names count dir file
    names=$(registry_list)
    count=$(printf '%s' "$names" | grep -c . || true)
    if [ "$count" -eq 0 ]; then
        warn "no Alliance Manager install is registered in $(registry_dir)."
        warn "Install with scripts/install.sh, or convert an existing clone with scripts/manage.sh migrate."
        return 1
    fi
    if [ -z "$want" ]; then
        if [ "$count" -gt 1 ]; then
            warn "several installs are registered — choose one by name: $(printf '%s' "$names" | tr '\n' ' ')"
            return 1
        fi
        want=$names
    fi
    file="$(registry_dir)/$want.conf"
    if [ ! -e "$file" ]; then
        warn "no install named '$want' is registered. Registered: $(printf '%s' "$names" | tr '\n' ' ')"
        return 1
    fi
    dir=$(registry_get "$want")
    if [ -z "$dir" ] || [ ! -f "$dir/docker-compose.yml" ]; then
        warn "install '$want' is registered at '$dir', which holds no docker-compose.yml."
        warn "It was probably moved: set APP_DIR in $file to its new location."
        return 1
    fi
    printf '%s\n' "$dir"
}
