#!/bin/bash
# shellcheck source-path=SCRIPTDIR
# scripts/manage.sh — update, migrate, back up and inspect an Alliance Manager install.
#
#   ./scripts/manage.sh update [--version vX.Y.Z | --asset FILE] [--yes]
#       Move the install — host files AND image, together — to the newest release, or to the
#       one named (which is also how to roll back). --asset takes a local host-files tarball.
#   ./scripts/manage.sh migrate [--asset FILE] [--name NAME] [--yes]
#       Convert an install from before v2.0.0 (a git clone, or an SCP copy) to versioned host
#       files, in place. Run it once, after `git pull`. Data is never moved.
#   ./scripts/manage.sh status      what this install runs, and whether it is up
#   ./scripts/manage.sh backup      the pre-update backup, on demand
#
# ── An update runs in two stages ──
# Stage 1 (cmd_update / cmd_migrate) is this file as it is on disk: the PREVIOUS release's
# copy. It resolves the target, downloads and verifies the host-files asset, unpacks it into
# a fresh staging directory ($APP_DIR/.staging/<tag>/), and `exec`s the STAGED copy of this
# script as `apply`. Stage 2 (cmd_apply) is therefore always the delivered release's own
# code, and it is what copies the new files over the old — this file included, which is safe
# because the process running is the staged copy. An updater that overwrites the file bash
# is executing (lastwar-private-docs#135) either runs the old code to the end or stops
# silently mid-run; nothing here depends on which.
#
# FROZEN CONTRACT — because stage 1 is always one release old, what it calls changes one
# release late: this argument list, and resolve_latest_release / download_asset /
# unpack_asset in lib/common.sh.
#
#     apply --app-dir DIR --target TAG --staging DIR [--migrate] [--yes] [--name NAME]
#
# A release may ADD arguments but must keep accepting these. apply warns about and ignores
# any argument it does not know, so a newer stage 1 driving an older apply — a rollback with
# --version — still works.
#
# ── Two anchors ──
# The operator-invoked commands derive APP_DIR from this file's location. apply NEVER does:
# it runs from the staging directory, where that derivation names the staging tree. It takes
# --app-dir, acts only on that directory, and sources its libraries from its own (staged)
# lib/.
set -eE
# Without this, bash runs every $(...) with `set -e` OFF: a failed step inside a function whose
# output is captured would be skipped, not stop the update.
shopt -s inherit_errexit

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"
# shellcheck source=lib/registry.sh
source "$SCRIPT_DIR/lib/registry.sh"
# shellcheck source=lib/preflight.sh
source "$SCRIPT_DIR/lib/preflight.sh"

AM_LEGACY_DIR=${AM_LEGACY_DIR:-/var/lib/lastwar}

usage() {
    sed -n '3,13p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# am_exec — exec, as a function so the tests can record the handoff instead of taking it.
am_exec() { exec "$@"; }

# dc — docker compose, run in the install directory, where it finds its files and .env.
dc() { (cd "$APP_DIR" && sudo docker compose "$@"); }

operator_app_dir() {
    APP_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
    export APP_DIR
}

require_install() {
    if [ ! -f "$APP_DIR/docker-compose.yml" ] || [ ! -f "$APP_DIR/.env" ]; then
        die "$APP_DIR has no docker-compose.yml and .env, so it is not an install directory"
    fi
}

# manage_preflight [EXTRA_TOOL...]
manage_preflight() {
    PF_TOOLS="curl tar sqlite3 sha256sum openssl $*"
    PF_LOG="$APP_DIR/manage.log"
    preflight_run not-root sudo tools docker compose-v2 docker-running disk
}

edge_message() {
    die "this install runs APP_VERSION=edge, the development image built from main. edge has no
host-files asset, so there are no host files to update it with. To take a newer edge image:
    sudo docker compose pull && sudo docker compose up -d
To return to releases: ./scripts/manage.sh update --version vX.Y.Z"
}

# --- Stage 1 ---------------------------------------------------------------------------------

# stage1 MIGRATE ARGS... — the part that is one release old. Keep it small.
stage1() {
    local migrate=$1 version='' asset='' yes=0 name='' tag staging dl
    shift
    while [ $# -gt 0 ]; do
        case $1 in
            --version) version=${2:-}; shift 2 ;;
            --asset) asset=${2:-}; shift 2 ;;
            --yes|-y) yes=1; shift ;;
            --name) name=${2:-}; shift 2 ;;
            -h|--help) usage; exit 0 ;;
            *) die "unknown option: $1 (see --help)" ;;
        esac
    done
    if [ -n "$version" ] && [ -n "$asset" ]; then
        die "--version and --asset name two different targets; give one"
    fi

    if [ -n "$asset" ]; then
        [ -f "$asset" ] || die "no such file: $asset"
        asset=$(cd "$(dirname "$asset")" && pwd)/$(basename "$asset")
        # A rehearsal build names a version that is not semver; --asset takes it as given.
        tag=$(tar -xzOf "$asset" ./HOST_FILES_VERSION 2>/dev/null || tar -xzOf "$asset" HOST_FILES_VERSION 2>/dev/null) \
            || die "$asset carries no HOST_FILES_VERSION: it is not a host-files asset"
        tag=$(printf '%s' "$tag" | tr -d '[:space:]')
    elif [ -n "$version" ]; then
        if [ "$version" = latest ]; then
            version=$(resolve_latest_release)
            [ -n "$version" ] || die "could not resolve the latest release; nothing changed"
        fi
        [ "$version" = edge ] && edge_message
        is_release_tag "$version" || die "$version is not a release tag (vX.Y.Z)"
        if version_lt "$version" "$AM_FIRST_ASSET_RELEASE"; then
            die "releases before $AM_FIRST_ASSET_RELEASE have no host-files asset, so manage.sh cannot move to $version.
To run that release's image anyway, set APP_VERSION=$version in .env, then:
    sudo docker compose pull && sudo docker compose up -d"
        fi
        tag=$version
    else
        if [ "$migrate" = 0 ] && [ "$(env_get APP_VERSION)" = edge ]; then
            edge_message
        fi
        tag=$(resolve_latest_release)
        [ -n "$tag" ] || die "could not resolve the latest release (no network, or GitHub refused); nothing changed — APP_VERSION stays $(env_get APP_VERSION)"
        # Between a v2.0.0 merge and its publication, "latest" is still a release with no
        # host files: say that, rather than failing the download with nothing to go on.
        if is_release_tag "$tag" && version_lt "$tag" "$AM_FIRST_ASSET_RELEASE"; then
            die "the newest published release, $tag, predates versioned host files ($AM_FIRST_ASSET_RELEASE has not been published yet); nothing changed"
        fi
        # "Newest" moving backwards (a release withdrawn) is never followed silently.
        local current
        current=$(env_get HOST_FILES_VERSION)
        if is_release_tag "$tag" && is_release_tag "$current" && version_lt "$tag" "$current"; then
            die "the newest published release, $tag, is older than this install's $current; nothing changed. To go back to it deliberately: ./scripts/manage.sh update --version $tag"
        fi
    fi
    # It becomes a directory name under .staging/ that is then emptied: `..` must never pass.
    [[ $tag =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || die "unusable release name: $tag"

    if [ "$migrate" = 0 ] && [ "$(env_get HOST_FILES_VERSION)" = "$tag" ] && [ "$(env_get APP_VERSION)" = "$tag" ]; then
        log "Already on $tag. Making sure the containers are up."
        dc up -d
        return 0
    fi

    staging="$APP_DIR/.staging/$tag"
    rm -rf "$staging"
    mkdir -p "$staging"
    if [ -n "$asset" ]; then
        unpack_asset "$asset" "$staging" || die "could not unpack $asset"
    else
        step "Downloading the $tag host files"
        dl=$(mktemp -d)
        download_asset "$tag" "$dl" || { rm -rf "$dl" "$staging"; die "could not download the $tag host files; nothing changed"; }
        unpack_asset "$dl/host-files.tar.gz" "$staging" || { rm -rf "$dl" "$staging"; die "the $tag download is not a host-files asset; nothing changed"; }
        rm -rf "$dl"
    fi

    local args=(apply --app-dir "$APP_DIR" --target "$tag" --staging "$staging")
    [ "$migrate" = 1 ] && args+=(--migrate)
    [ "$yes" = 1 ] && args+=(--yes)
    [ -n "$name" ] && args+=(--name "$name")
    log "Handing over to the $tag updater."
    am_exec "$staging/scripts/manage.sh" "${args[@]}"
}

cmd_update() {
    operator_app_dir
    require_install
    if ! env_has HOST_FILES_VERSION; then
        die "this install has not been migrated to versioned host files (v2.0.0). Run:
    git pull && ./scripts/manage.sh migrate
(see the v2.0.0 release notes)"
    fi
    manage_preflight
    stage1 0 "$@"
}

cmd_migrate() {
    operator_app_dir
    require_install
    if env_has HOST_FILES_VERSION; then
        die "this install is already migrated (host files $(env_get HOST_FILES_VERSION)) — use ./scripts/manage.sh update"
    fi
    if [ -d "$APP_DIR/.git" ] && ! command -v git >/dev/null 2>&1; then
        die "this install is a git clone but git is not installed. Install git (sudo apt-get install git), or remove .git to migrate it as a copy with no history."
    fi
    # crontab: a migrate installs the nightly backup, a root crontab line.
    manage_preflight crontab
    stage1 1 "$@"
}

# --- Backups ---------------------------------------------------------------------------------

# backup_install — app_<ts>.tar.gz of the install directory (without .git, .staging and data/
# — the database is covered by the consistent .backup beside it, and a tar of a live WAL
# database is not; data/ can also hold gigabytes of OCR archive) and db_<ts>.db. uploads/
# stays in: it is the alliance's files, and this is their only backup. Each pool keeps its
# newest ten. The nightly pool is the root-owned helper's, not this.
backup_install() {
    local ts db x prefix
    ts=$(date +%Y%m%d_%H%M%S)
    BACKUP_TS=$ts
    sudo mkdir -p "$AM_BACKUP_DIR"
    step "Backing up to $AM_BACKUP_DIR"
    sudo tar -czf "$AM_BACKUP_DIR/app_$ts.tar.gz" -C "$APP_DIR" \
        --exclude=./.git --exclude=./.staging --exclude=./data .
    sudo chmod 600 "$AM_BACKUP_DIR/app_$ts.tar.gz"
    db="$APP_DIR/data/alliance.db"
    # sudo: the database belongs to the container's user, not the operator.
    if sudo test -f "$db"; then
        sudo sqlite3 "$db" ".backup '$AM_BACKUP_DIR/db_$ts.db'"
        sudo chmod 600 "$AM_BACKUP_DIR/db_$ts.db"
        # Opening the database can create -wal/-shm as root while the container is down.
        for x in "$db-wal" "$db-shm"; do
            if sudo test -e "$x"; then
                sudo chown --reference="$db" "$x"
            fi
        done
    fi
    for prefix in app_ db_; do
        sudo find "$AM_BACKUP_DIR" -maxdepth 1 -name "$prefix*" -printf '%T@ %p\n' \
            | sort -rn | tail -n +11 | cut -d' ' -f2- | xargs -r sudo rm -f
    done
    log "  $AM_BACKUP_DIR/app_$ts.tar.gz"
    if sudo test -f "$AM_BACKUP_DIR/db_$ts.db"; then
        log "  $AM_BACKUP_DIR/db_$ts.db"
    fi
}

cmd_backup() {
    operator_app_dir
    require_install
    backup_install
}

# --- Stage 2: apply --------------------------------------------------------------------------

# Rollback. Before the first change, every path apply may touch — the new release's files,
# the previous release's (which it may delete), .env and .host-files.list — is copied aside.
# If apply exits non-zero while armed, for any reason (a failed step, `set -e`, Ctrl-C), the
# EXIT trap puts those paths back exactly and removes any that did not exist. The running
# containers are not touched until after the last step that can fail, so a rollback leaves
# the old files beside the old containers: consistent.
snapshot_take() {
    local p
    mkdir -p "$APP_DIR/.staging"
    SNAP=$(mktemp -d "$APP_DIR/.staging/rollback.XXXXXX")
    : > "$SNAP/present"
    : > "$SNAP/absent"
    for p in "$@"; do
        if [ -e "$APP_DIR/$p" ] || [ -L "$APP_DIR/$p" ]; then
            mkdir -p "$SNAP/files/$(dirname "$p")"
            cp -a "$APP_DIR/$p" "$SNAP/files/$p"
            printf '%s\n' "$p" >> "$SNAP/present"
        else
            printf '%s\n' "$p" >> "$SNAP/absent"
        fi
    done
}

snapshot_restore() {
    local p restored=0 removed=0
    while IFS= read -r p; do
        mkdir -p "$APP_DIR/$(dirname "$p")"
        cp -a --remove-destination "$SNAP/files/$p" "$APP_DIR/$p"
        restored=$((restored + 1))
    done < "$SNAP/present"
    while IFS= read -r p; do
        if [ -e "$APP_DIR/$p" ] || [ -L "$APP_DIR/$p" ]; then
            rm -f "$APP_DIR/$p"
            removed=$((removed + 1))
        fi
        (cd "$APP_DIR" && rmdir -p --ignore-fail-on-non-empty "$(dirname "$p")" 2>/dev/null) || true
    done < "$SNAP/absent"
    log "Restored $restored file(s) and removed $removed the update had added."
}

apply_on_exit() {
    local status=$1
    [ "${APPLY_ARMED:-0}" = 1 ] || return 0
    APPLY_ARMED=0
    set +e
    printf '\n%sThe update failed%s (exit %s) — putting the install back as it was.\n' "$C_RED" "$C_NC" "$status" >&2
    snapshot_restore >&2
    if [ -n "${CADDY_BACKUP:-}" ]; then
        sudo install -m 0644 "$CADDY_BACKUP" "$AM_CADDYFILE" && sudo systemctl reload caddy \
            && log "Restored $AM_CADDYFILE from $CADDY_BACKUP." >&2
    fi
    # What a migrate added outside the install directory, only where this run created it: a
    # registry entry naming an install that is not migrated would mislead the next install.sh,
    # and a helper or crontab line nothing registered feeds would fail every night.
    if [ -n "${CREATED_REGISTRY:-}" ]; then
        registry_remove "$CREATED_REGISTRY" && log "Removed the registry entry '$CREATED_REGISTRY' this run added." >&2
    fi
    if [ "${CREATED_HELPER:-0}" = 1 ]; then
        sudo rm -f "$AM_BACKUP_HELPER"
    fi
    if [ "${CREATED_CRONTAB:-0}" = 1 ]; then
        sudo crontab -l 2>/dev/null | grep -vF "$AM_BACKUP_HELPER" | sudo crontab -
    fi
    printf 'Nothing else changed: the containers were not restarted. Backups: %s/*_%s*\n' \
        "$AM_BACKUP_DIR" "${BACKUP_TS:-}" >&2
    rm -rf "$SNAP"
    remove_staging
}

# remove_staging — the unpacked release, once finished with. Only ever a directory under
# $APP_DIR/.staging/: --staging arrives on a command line, and this is an rm -rf.
remove_staging() {
    case ${STAGING_DIR:-} in
        "$APP_DIR/.staging/"?*) rm -rf "$STAGING_DIR" ;;
    esac
    rmdir "$APP_DIR/.staging" 2>/dev/null || true
}

# fail_apply MESSAGE — stop the apply with a reason; the EXIT trap does the rollback.
fail_apply() {
    printf '%sError:%s %s\n' "$C_RED" "$C_NC" "$*" >&2
    exit 1
}

# copy_file SRC DST — one file of the new release into place. Unlink-then-create
# (--remove-destination), so nothing still reading the old file sees it change under it.
copy_file() {
    mkdir -p "$(dirname "$2")"
    cp -a --remove-destination "$1" "$2"
}

# remove_paths LIST... — delete these install files, then any directory they leave empty.
remove_paths() {
    local p count=0
    for p in "$@"; do
        if [ -e "$APP_DIR/$p" ] || [ -L "$APP_DIR/$p" ]; then
            rm -f "$APP_DIR/$p"
            count=$((count + 1))
        fi
        (cd "$APP_DIR" && rmdir -p --ignore-fail-on-non-empty "$(dirname "$p")" 2>/dev/null) || true
    done
    REMOVED=$count
}

# set_minus A_FILE B_FILE — the lines of A that are not in B.
set_minus() {
    if [ -s "$2" ]; then
        grep -vxF -f "$2" "$1" || true
    else
        cat "$1"
    fi
}

# Never removed, whatever a clone's history says: an operator can force-track their own
# override, and .env is theirs by definition.
PROTECTED_PATHS='docker-compose.override.yml
.env'

# preserve_git_changes — before a migration deletes a clone's history, save anything the
# operator would lose with it: uncommitted edits to tracked files, stashes (the old updater's
# `stash pop || true` could leave a customisation parked in one), and local commits. Prints
# the directory, or nothing.
preserve_git_changes() {
    local dirty stashes ahead='' tmp dir n
    dirty=$(git -C "$APP_DIR" status --porcelain --untracked-files=no)
    stashes=$(git -C "$APP_DIR" stash list)
    if git -C "$APP_DIR" rev-parse --abbrev-ref '@{upstream}' >/dev/null 2>&1; then
        ahead=$(git -C "$APP_DIR" log --oneline '@{upstream}..HEAD')
    fi
    [ -z "$dirty$stashes$ahead" ] && return 0
    tmp=$(mktemp -d)
    if [ -n "$dirty" ]; then
        git -C "$APP_DIR" diff HEAD > "$tmp/uncommitted.patch"
    fi
    if [ -n "$stashes" ]; then
        n=0
        while IFS= read -r _; do
            git -C "$APP_DIR" stash show -p "stash@{$n}" > "$tmp/stash-$n.patch"
            n=$((n + 1))
        done <<<"$stashes"
    fi
    if [ -n "$ahead" ]; then
        git -C "$APP_DIR" format-patch -q -o "$tmp/local-commits" '@{upstream}..HEAD'
    fi
    dir="$AM_BACKUP_DIR/migrate_${BACKUP_TS}"
    sudo mkdir -p "$dir"
    sudo cp -r "$tmp"/. "$dir"/
    rm -rf "$tmp"
    printf '%s' "$dir"
}

cmd_apply() {
    local app_dir='' target='' staging='' migrate=0 yes=0 name=default
    while [ $# -gt 0 ]; do
        case $1 in
            --app-dir) app_dir=$2; shift 2 ;;
            --target) target=$2; shift 2 ;;
            --staging) staging=$2; shift 2 ;;
            --migrate) migrate=1; shift ;;
            --yes) yes=1; shift ;;
            --name) name=$2; shift 2 ;;
            *) warn "apply: ignoring an argument this release does not know: $1"; shift ;;
        esac
    done
    if [ -z "$app_dir" ] || [ -z "$target" ] || [ -z "$staging" ]; then
        die "apply is the second stage of an update; run ./scripts/manage.sh update"
    fi
    APP_DIR=$app_dir
    STAGING_DIR=$staging
    export APP_DIR
    cd "$APP_DIR"
    # However this ends — a refusal below included — the unpacked release is not left behind.
    trap 'remove_staging' EXIT
    AM_WRITER="manage.sh $target"
    registry_valid_name "$name" || die "--name may hold letters, digits, . _ and - only"

    local shipped previous p summary=() patches helper_out
    shipped=$(mktemp) previous=$(mktemp)
    # Paths relative to the staging root, as every set they are compared with is.
    (cd "$staging" && find . -type f | sed 's|^\./||' | sort) > "$shipped"
    if [ "$migrate" = 1 ]; then
        if [ -d .git ]; then
            git -C "$APP_DIR" ls-files | sort > "$previous"
        fi
    elif [ -f .host-files.list ]; then
        sort .host-files.list > "$previous"
    fi

    # 1. Backup.
    backup_install

    # 2. Migrate only: refuse, or preserve, before anything changes.
    if [ "$migrate" = 1 ]; then
        if systemctl is-active --quiet lastwar.service 2>/dev/null \
            || { [ -f "$AM_LEGACY_DIR/alliance.db" ] && ! sudo test -f "$APP_DIR/data/alliance.db"; }; then
            die "this host still runs, or still has the data of, the pre-Docker lastwar.service
($AM_LEGACY_DIR). This migration does not copy that data. Convert the server to Docker with the
v1.1.0 scripts first:
    git checkout v1.1.0 && ./scripts/update.sh
then come back to this release: git checkout main && git pull && ./scripts/manage.sh migrate"
        fi
        if [ -f docker-compose.override.yml ] \
            && grep -Eq '^[[:space:]]*build:|\./templates|\./static' docker-compose.override.yml; then
            die "docker-compose.override.yml builds the image from source or mounts ./templates or ./static,
which this migration removes. That file is the development override
(deploy/docker-compose.override.yml.example); a production host does not need it. Move it
aside and run migrate again. For LAN access without a proxy, set BIND_ADDR=0.0.0.0 in .env instead."
        fi
        if [ -d .git ]; then
            patches=$(preserve_git_changes)
            if [ -n "$patches" ]; then
                log "This clone has local changes; they are saved as patches in $patches"
                if [ "$yes" != 1 ]; then
                    die "the migration replaces the tracked files those changes touched. Check the patches, then
re-run with --yes to continue:  ./scripts/manage.sh migrate --yes"
                fi
                summary+=("local changes saved as patches in $patches")
            fi
        fi
    fi

    # From here to the marker, any failure puts everything back.
    local -a touched
    mapfile -t touched < <(sort -u "$shipped" "$previous")
    snapshot_take "${touched[@]}" .env .host-files.list
    APPLY_ARMED=1
    trap 'apply_on_exit $?' EXIT
    trap 'exit 130' INT TERM

    # 3. Pin, before the pull: compose resolves ${APP_VERSION} from .env when it runs.
    env_set APP_VERSION "$target"

    # 4. Nothing in the install changes until the target image is held locally.
    step "Pulling the $target images"
    dc pull || fail_apply "could not pull the $target images"

    # 5 / 6. Remove what the previous release delivered and this one does not. Bounded to that
    # set, so a file the operator created is never a candidate.
    local -a dropped
    mapfile -t dropped < <(set_minus "$previous" "$shipped" | grep -vxF "$PROTECTED_PATHS" || true)
    REMOVED=0
    if [ ${#dropped[@]} -gt 0 ]; then
        remove_paths "${dropped[@]}"
    fi
    if [ "$migrate" = 1 ]; then
        if [ -d .git ]; then
            summary+=("removed $REMOVED file(s) the clone had and a host does not need")
        else
            summary+=("no .git: a copy with no history, so nothing was removed — files overlaid")
        fi
    elif [ "$REMOVED" -gt 0 ]; then
        summary+=("removed $REMOVED file(s) this release no longer ships")
    fi

    # 7. The new release's files, then the proof that they still form a valid stack.
    step "Installing the $target host files"
    while IFS= read -r p; do
        copy_file "$staging/$p" "$APP_DIR/$p"
    done < "$shipped"
    cp "$shipped" .host-files.list
    dc config -q || fail_apply "the $target compose files do not form a valid stack with this install's .env and overrides (above)"

    # 8. .env keys a release needs and an old install may lack. No prompts: a prompt inside
    # an updater is what makes it unautomatable.
    env_ensure CREDENTIAL_ENCRYPTION_KEY "$(openssl rand -hex 32)"
    env_ensure OCR_BACKEND_MODE cloud
    env_ensure OCR_ARCHIVE_DIR ''

    if [ "$migrate" = 1 ]; then
        # What this run creates outside the install directory is undone by a rollback.
        [ -e "$(registry_dir)/$name.conf" ] || CREATED_REGISTRY=$name
        registry_write "$name" "$APP_DIR" || fail_apply "could not register the install (above)"
        summary+=("registered as '$name' in $(registry_dir)")
        [ -e "$AM_BACKUP_HELPER" ] || CREATED_HELPER=1
        sudo crontab -l 2>/dev/null | grep -qF "$AM_BACKUP_HELPER" || CREATED_CRONTAB=1
        helper_out=$(ensure_backup_helper)
        summary+=("${helper_out#Nightly backup helper: }")
        # Only under PRODUCTION=true: a plain-HTTP LAN install with no proxy is exactly where
        # 1 is wrong, and there the app's default of 0 applies.
        if [ "$(env_get PRODUCTION)" = true ] && ! env_has TRUSTED_PROXY_COUNT; then
            env_set TRUSTED_PROXY_COUNT 1
            summary+=("TRUSTED_PROXY_COUNT=1 written (this install runs behind a proxy)")
        fi
        # The root update.sh shim: a tracked file on a clone (so already removed above), but
        # an SCP-style copy still has it.
        if [ -f update.sh ] && grep -q 'Deprecated shim' update.sh; then
            rm -f update.sh
        fi
    fi

    # 9. The proxy config, if this host runs our Caddy and the release ships a newer revision.
    render_caddy "$target"

    # 10. Migrate only: the history goes last, so every earlier failure still had it.
    if [ "$migrate" = 1 ] && [ -d .git ]; then
        rm -rf .git || warn "could not remove .git completely; remove what is left by hand"
    fi

    # 11. The marker, after everything that can fail and before `up`, so the container about
    # to be created sees it. An `up` that fails after this is retried by the next update.
    env_set HOST_FILES_VERSION "$target"
    APPLY_ARMED=0
    trap 'remove_staging' EXIT
    trap - INT TERM
    rm -rf "$SNAP"

    # 12. Start what was pulled.
    step "Starting $target"
    if ! dc up -d; then
        warn "the containers did not start. The install is on $target and consistent; see"
        warn "    cd $APP_DIR && sudo docker compose logs alliance-manager"
        warn "and run ./scripts/manage.sh update again to retry."
        exit 1
    fi
    sudo docker image prune -f >/dev/null || true

    # 13. Tidy.
    remove_staging
    rm -f "$shipped" "$previous"

    log ""
    log "${C_GREEN}${C_BOLD}This install is on $target — host files and image.${C_NC}"
    for p in "${summary[@]}"; do
        log "  - $p"
    done
    if [ "$migrate" = 1 ]; then
        log "From now on, update with:  ./scripts/manage.sh update"
    fi
}

# render_caddy TARGET — re-render /etc/caddy/Caddyfile when the shipped template's revision is
# higher than the deployed file's. Rendered to a temp file and validated THERE; the live file
# is replaced only once validation passes, so a Caddy restart can never load an unchecked
# config. A deployed revision HIGHER than the template (a rollback) is left alone: revisions
# only ever add hosts and headers, and a newer proxy config in front of an older image has
# never been the failing combination. Problems here are warnings — the proxy keeps its
# working config, and the next update tries again.
render_caddy() {
    local deployed shipped app collab tmp out validate=(sudo caddy)
    [ -f "$AM_CADDYFILE" ] || return 0
    deployed=$(caddyfile_rev "$AM_CADDYFILE")
    shipped=$(caddyfile_rev "$APP_DIR/deploy/Caddyfile")
    if [ "$deployed" -ge "$shipped" ]; then
        [ "$deployed" -gt "$shipped" ] && log "Caddyfile: the deployed rev $deployed is newer than $1's rev $shipped — left as it is."
        return 0
    fi
    app=$(env_get APP_DOMAIN)
    collab=$(env_get COLLABORA_DOMAIN)
    if placeholder_domain "$app" || ! valid_domain "$app"; then
        warn "Caddyfile not updated: set APP_DOMAIN in .env to this install's domain, then run ./scripts/manage.sh update again."
        return 0
    fi
    if placeholder_domain "$collab" || ! valid_domain "$collab"; then
        warn "Caddyfile not updated: set COLLABORA_DOMAIN in .env, then run ./scripts/manage.sh update again."
        return 0
    fi
    tmp=$(mktemp)
    render_caddyfile "$APP_DIR/deploy/Caddyfile" "$app" "$collab" > "$tmp"
    chmod 0644 "$tmp"
    if id caddy >/dev/null 2>&1; then
        validate=(sudo -u caddy caddy)
    fi
    if ! out=$("${validate[@]}" validate --adapter caddyfile --config "$tmp" 2>&1); then
        printf '%s\n' "$out" >&2
        warn "the rev $shipped Caddyfile did not validate, so $AM_CADDYFILE was left as it is. The rendered file is $tmp"
        return 0
    fi
    CADDY_BACKUP="$AM_CADDYFILE.backup_$(date +%Y%m%d_%H%M%S)"
    sudo cp -p "$AM_CADDYFILE" "$CADDY_BACKUP"
    sudo install -m 0644 "$tmp" "$AM_CADDYFILE"
    rm -f "$tmp"
    if ! sudo systemctl reload caddy; then
        sudo install -m 0644 "$CADDY_BACKUP" "$AM_CADDYFILE"
        sudo systemctl reload caddy || true
        warn "Caddy would not reload the rev $shipped Caddyfile; the previous one is back in place."
        CADDY_BACKUP=''
        return 0
    fi
    log "Caddyfile updated to rev $shipped (the previous one is $CADDY_BACKUP)."
}

# --- status ----------------------------------------------------------------------------------

cmd_status() {
    local name deployed template image line
    operator_app_dir
    require_install
    log "Install directory:  $APP_DIR"
    if name=$(registry_name_for "$APP_DIR"); then
        log "Registered as:      $name ($(registry_dir)/$name.conf)"
    else
        log "Registered as:      (not registered — run ./scripts/manage.sh migrate)"
    fi
    log "Image (APP_VERSION):       $(env_get APP_VERSION)"
    log "Host files:                $(env_get HOST_FILES_VERSION || true)$(env_has HOST_FILES_VERSION || printf 'not migrated')"
    template=$(caddyfile_rev "$APP_DIR/deploy/Caddyfile")
    if [ -f "$AM_CADDYFILE" ]; then
        deployed=$(caddyfile_rev "$AM_CADDYFILE")
        log "Caddyfile:                 rev $deployed deployed, rev $template shipped$([ "$deployed" != "$template" ] && printf ' (differs)')"
    else
        log "Caddyfile:                 none at $AM_CADDYFILE (another proxy?)"
    fi
    image=$(sudo docker inspect -f '{{.Config.Image}}' alliance-manager 2>/dev/null || true)
    log "Running image:             ${image:-(container not running)}"
    line=$(dc logs --no-log-prefix alliance-manager 2>/dev/null | grep 'Initializing Alliance Manager server' | tail -n1 || true)
    [ -n "$line" ] && log "Startup line:              $line"
    log ""
    dc ps
}

main() {
    local cmd=${1:-}
    [ $# -gt 0 ] && shift
    case $cmd in
        update) cmd_update "$@" ;;
        migrate) cmd_migrate "$@" ;;
        status) cmd_status ;;
        backup) cmd_backup ;;
        apply) cmd_apply "$@" ;;
        ''|-h|--help|help) usage ;;
        *) die "unknown command: $cmd (update, migrate, status, backup)" ;;
    esac
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
