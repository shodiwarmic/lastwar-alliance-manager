# shellcheck shell=bash
# shellcheck disable=SC2034  # tests set globals that the functions under test read
# Tests for scripts/manage.sh — the two-stage update and the v2.0.0 migration.

# The host files a release ships: the manifest build-host-files.sh reads, from this checkout.
mapfile -t SHIPPED_FILES < <(sed 's/#.*//; s/[[:space:]]*$//' "$REPO_ROOT/.github/host-files.manifest" | grep -v '^$')

# make_staging DIR TAG [SKIP] — DIR laid out as an unpacked asset for TAG, optionally
# without the file SKIP (a release that stopped shipping it).
make_staging() {
    local dir=$1 tag=$2 skip=${3:-} f
    mkdir -p "$dir"
    for f in "${SHIPPED_FILES[@]}"; do
        [ "$f" = "$skip" ] && continue
        mkdir -p "$dir/$(dirname "$f")"
        cp -p "$REPO_ROOT/$f" "$dir/$f"
    done
    echo "$tag" > "$dir/HOST_FILES_VERSION"
    (cd "$dir" && { find . -type f | sed 's|^\./||'; echo .host-files.list; } | sort -u > .host-files.list)
}

# make_asset TAG — a host-files tarball for TAG; prints its path.
make_asset() {
    local src="$TEST_TMP/asset-src-$1"
    make_staging "$src" "$1"
    tar -czf "$TEST_TMP/host-files-$1.tar.gz" -C "$src" .
    printf '%s' "$TEST_TMP/host-files-$1.tar.gz"
}

# make_clone — a pre-v2.0.0 install at $APP: a git clone that has just pulled v2.0.0's
# scripts (so scripts/manage.sh exists), with the files git delivered and the ones it never
# did (.env, data/, uploads/, an override).
make_clone() {
    APP="$TEST_TMP/app"
    mkdir -p "$APP"
    make_staging "$APP" unused
    rm -f "$APP/HOST_FILES_VERSION" "$APP/.host-files.list"
    mkdir -p "$APP/templates" "$APP/internal/app" "$APP/docs"
    echo '<p>page</p>' > "$APP/templates/x.html"
    echo 'package app' > "$APP/internal/app/a.go"
    echo '# docs' > "$APP/docs/README.md"
    printf '#!/bin/bash\n# Deprecated shim — the real script moved to scripts/update.sh.\n' > "$APP/update.sh"
    echo '#!/bin/bash' > "$APP/scripts/refresh-dev-db.sh"
    printf '.env\ndata/\nuploads/\ndocker-compose.override.yml\n' > "$APP/.gitignore"
    git -C "$APP" init -q -b main
    git -C "$APP" -c user.email=t@t -c user.name=t add -A
    git -C "$APP" -c user.email=t@t -c user.name=t commit -qm "v1.1.0-era clone"
    # An upstream, so "commits ahead" is a question with an answer.
    git clone -q --bare "$APP" "$TEST_TMP/origin.git"
    git -C "$APP" remote add origin "$TEST_TMP/origin.git"
    git -C "$APP" fetch -q origin
    git -C "$APP" branch -q -u origin/main

    mkdir -p "$APP/data" "$APP/uploads"
    printf 'SESSION_KEY=%064d\nPRODUCTION=true\nAPP_DOMAIN=app.warmic.test\nCOLLABORA_DOMAIN=collabora.warmic.test\nAPP_VERSION=v1.1.0\nTRUSTED_ORIGINS=app.warmic.test, localhost:8080\n' 1 > "$APP/.env"
    echo "the database" > "$APP/data/alliance.db"
    echo "a member's file" > "$APP/uploads/doc.pdf"
    printf 'services:\n  alliance-manager:\n    environment:\n      - EXTRA=1\n' > "$APP/docker-compose.override.yml"
    echo "operator notes" > "$APP/NOTES.txt"
    ENV_FILE="$APP/.env"
}

# The host: everything present and running; docker records what .env said at each step.
fake_host() {
    stub_respond systemctl <<'SH'
case "$*" in *lastwar.service*) exit 3 ;; esac
exit 0
SH
    stub_respond openssl <<'SH'
printf '%064d\n' 7
SH
    stub_respond docker <<'SH'
env_val() { grep -m1 "^$1=" .env 2>/dev/null | cut -d= -f2-; }
case "$*" in
    "compose "*pull)
        echo "# pull saw APP_VERSION=$(env_val APP_VERSION) marker=$(env_val HOST_FILES_VERSION)" >> "$STUB_LOG"
        [ -f "$TEST_TMP/fail-pull" ] && exit 1 ;;
    "compose "*"config -q")
        [ -f "$TEST_TMP/fail-config" ] && { echo "invalid compose project" >&2; exit 1; } ;;
    "compose "*"config --format json")
        # The merged stack's mounts: a test's own in mounts.json, else the ./data bind.
        if [ -f "$TEST_TMP/mounts.json" ]; then cat "$TEST_TMP/mounts.json"; else
            printf '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"%s/data","target":"/app/data"},{"type":"bind","source":"%s/uploads","target":"/app/uploads"}]}}}\n' "$PWD" "$PWD"
        fi ;;
    "compose "*"config --images")
        if [ -f "$TEST_TMP/images" ]; then cat "$TEST_TMP/images"; else
            printf 'ghcr.io/shodiwarmic/lastwar-alliance-manager:%s\ncollabora/code@sha256:feed\n' "$(env_val APP_VERSION)"
        fi ;;
    "image inspect "*)
        # Present unless a test listed it in absent-images.
        [ -f "$TEST_TMP/absent-images" ] && grep -qxF "${3:-}" "$TEST_TMP/absent-images" && exit 1 ;;
    "compose up -d")
        echo "# up saw marker=$(env_val HOST_FILES_VERSION)" >> "$STUB_LOG"
        [ -f "$TEST_TMP/fail-up" ] && exit 1 ;;
esac
exit 0
SH
    stub_respond caddy <<'SH'
[ -f "$TEST_TMP/fail-validate" ] && exit 1
exit 0
SH
    # shellcheck source=../../scripts/manage.sh
    source "$APP/scripts/manage.sh"
    avail_kb() { echo $((50 * 1024 * 1024)); }
    docker_root_dir() { echo /; }
}

# tree_state DIR — every path under DIR with its content hash (the staging area excluded).
tree_state() {
    (cd "$1" && find . -path ./.staging -prune -o -print | LC_ALL=C sort | while IFS= read -r f; do
        if [ -f "$f" ]; then printf '%s %s\n' "$f" "$(md5sum < "$f" | cut -c1-32)"; else printf '%s\n' "$f"; fi
    done)
}

apply_migrate() {  # apply_migrate [extra args] — stage 2 of a migration to v9.9.9
    make_staging "$TEST_TMP/stage" v9.9.9
    run cmd_apply --app-dir "$APP" --target v9.9.9 --staging "$TEST_TMP/stage" --migrate "$@"
}

# --- Stage 1 ---------------------------------------------------------------------------------

test_stage1_stages_the_asset_and_hands_over_to_the_staged_copy() {
    make_clone
    fake_host
    local asset
    asset=$(make_asset v9.9.9)
    am_exec() { printf '%s\n' "$@" > "$TEST_TMP/exec-args"; }
    run cmd_migrate --asset "$asset"
    assert_status 0
    assert_file "$APP/.staging/v9.9.9/scripts/manage.sh"
    [ -x "$APP/.staging/v9.9.9/scripts/manage.sh" ] || _fail "the staged manage.sh cannot be exec'd"
    assert_eq "$(head -n1 "$TEST_TMP/exec-args")" "$APP/.staging/v9.9.9/scripts/manage.sh"
    assert_eq "$(sed -n '2,9p' "$TEST_TMP/exec-args" | tr '\n' ' ')" \
        "apply --app-dir $APP --target v9.9.9 --staging $APP/.staging/v9.9.9 --migrate "
    assert_eq "$(env_get APP_VERSION)" v1.1.0 "stage 1 changes nothing in the install"
}

test_stage1_update_refuses_an_unmigrated_install() {
    make_clone
    fake_host
    run cmd_update
    assert_status 1
    assert_contains "$OUT" "git pull && ./scripts/manage.sh migrate"
}

test_migrate_refuses_an_install_already_migrated() {
    make_clone
    fake_host
    echo HOST_FILES_VERSION=v9.9.9 >> "$APP/.env"
    run cmd_migrate
    assert_status 1
    assert_contains "$OUT" "already migrated"
}

test_migrate_needs_git_for_a_clone() {
    make_clone
    fake_host
    local bin="$TEST_TMP/nogit" t
    mkdir -p "$bin"
    for t in dirname cat grep sed tr flock; do ln -s "$(command -v $t)" "$bin/$t"; done
    PATH="$bin" run cmd_migrate
    assert_status 1
    assert_contains "$OUT" "git is not installed"
}

test_update_at_the_same_version_just_runs_up() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    env_set APP_VERSION v9.9.9
    stub_respond curl <<'SH'
printf '{\n  "tag_name": "v9.9.9"\n}\n'
SH
    am_exec() { _fail "no handover expected"; }
    run cmd_update
    assert_status 0
    assert_contains "$OUT" "Already on v9.9.9"
    assert_called "docker compose up -d"
}

test_update_refuses_edge_without_touching_env() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    env_set APP_VERSION edge
    local before
    before=$(cat "$APP/.env")
    run cmd_update
    assert_status 1
    assert_contains "$OUT" "edge"
    assert_eq "$(cat "$APP/.env")" "$before"
}

test_update_refuses_a_release_with_no_asset() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    run cmd_update --version v1.1.0
    assert_status 1
    assert_contains "$OUT" "have no host-files asset"
    assert_contains "$OUT" "docker compose pull"
}

test_update_unresolved_release_keeps_the_pin() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v2.0.0\n' >> "$APP/.env"
    env_set APP_VERSION v2.0.0
    stub_respond curl <<'SH'
exit 22
SH
    run cmd_update
    assert_status 1
    assert_contains "$OUT" "APP_VERSION stays v2.0.0"
    assert_eq "$(env_get APP_VERSION)" v2.0.0
}

test_migrate_before_v2_is_published_says_so() {
    make_clone
    fake_host
    stub_respond curl <<'SH'
printf '{\n  "tag_name": "v1.1.0"\n}\n'
SH
    am_exec() { _fail "no handover expected"; }
    run cmd_migrate
    assert_status 1
    assert_contains "$OUT" "has not been published yet"
    assert_not_called "curl -fsSL --max-time 300"
}

test_update_never_follows_latest_backwards() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v2.1.0\n' >> "$APP/.env"
    env_set APP_VERSION v2.1.0
    stub_respond curl <<'SH'
printf '{\n  "tag_name": "v2.0.5"\n}\n'
SH
    am_exec() { _fail "no handover expected"; }
    run cmd_update
    assert_status 1
    assert_contains "$OUT" "is older than this install's v2.1.0"
    assert_contains "$OUT" "--version v2.0.5"
    assert_eq "$(env_get APP_VERSION)" v2.1.0
}

test_stage1_refuses_a_tag_that_is_a_path() {
    make_clone
    fake_host
    local src="$TEST_TMP/evil"
    make_staging "$src" ".."
    tar -czf "$TEST_TMP/evil.tar.gz" -C "$src" .
    run cmd_migrate --asset "$TEST_TMP/evil.tar.gz"
    assert_status 1
    assert_contains "$OUT" "unusable release name"
    assert_file "$APP/docker-compose.yml"
}

# --- Stage 2: migrate ------------------------------------------------------------------------

test_migrate_prunes_exactly_what_git_delivered_and_the_asset_does_not() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    # Tracked, not shipped: gone, with the directories they leave empty.
    assert_no_file "$APP/templates/x.html"
    assert_no_dir "$APP/templates"
    assert_no_dir "$APP/internal"
    assert_no_dir "$APP/docs"
    assert_no_file "$APP/update.sh" "the root shim"
    assert_no_file "$APP/scripts/refresh-dev-db.sh" "dev tooling is not a host file"
    assert_no_file "$APP/.gitignore"
    assert_no_dir "$APP/.git"
    # Never delivered by git: untouched.
    assert_eq "$(cat "$APP/data/alliance.db")" "the database"
    assert_eq "$(cat "$APP/uploads/doc.pdf")" "a member's file"
    assert_file "$APP/docker-compose.override.yml"
    assert_file "$APP/NOTES.txt"
    # Shipped: present.
    assert_file "$APP/scripts/lib/preflight.sh"
    assert_eq "$(cat "$APP/HOST_FILES_VERSION")" v9.9.9
    assert_eq "$(env_get HOST_FILES_VERSION)" v9.9.9
    assert_eq "$(env_get APP_VERSION)" v9.9.9
    assert_file "$APP/.host-files.list"
    assert_contains "$(cat "$APP/.host-files.list")" "scripts/manage.sh"
    assert_no_dir "$APP/.staging"
    # Registered, with the nightly helper and one crontab line.
    assert_eq "$(registry_get default)" "$APP"
    assert_file "$AM_BACKUP_HELPER"
    assert_eq "$(grep -c backup-lastwar "$STUB_DIR/crontab")" 1
    assert_contains "$OUT" "removed"
    # The summary line keeps its subject: not a bare "written".
    assert_contains "$OUT" "  - Nightly backup helper: written; crontab line added"
}

test_migrate_orders_pin_pull_marker_up() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    grep -q '^# pull saw APP_VERSION=v9.9.9 marker=$' "$STUB_LOG" \
        || _fail "the pull must see the new pin and no marker: $(grep '^# pull' "$STUB_LOG")"
    grep -q '^# up saw marker=v9.9.9$' "$STUB_LOG" || _fail "up must see the marker"
    [ "$(line_of 'docker compose .* pull$')" -lt "$(line_of '^docker compose config -q')" ] || _fail "files copied before the pull"
}

# pull_call — the logged `docker compose … pull` call, arguments only.
pull_call() { stub_calls docker | grep ' pull$' | head -n1; }

test_apply_pulls_with_the_staged_compose_files() {
    make_clone
    fake_host
    rm "$APP/docker-compose.override.yml"
    apply_migrate
    assert_status 0
    assert_eq "$(pull_call)" "compose --project-directory $APP -f $TEST_TMP/stage/docker-compose.yml pull"
    stub_calls docker | grep -q "^compose --project-directory $APP -f $TEST_TMP/stage/docker-compose.yml config -q$" \
        || _fail "the staged stack should be validated before the pull: $(stub_calls docker | tr '\n' '|')"
}

test_apply_pulls_both_staged_files_of_a_local_ocr_install() {
    make_clone
    fake_host
    printf 'COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml\n' >> "$APP/.env"
    apply_migrate
    assert_status 0
    assert_eq "$(pull_call)" "compose --project-directory $APP -f $TEST_TMP/stage/docker-compose.yml -f $TEST_TMP/stage/docker-compose.local-ocr.yml pull" \
        "with COMPOSE_FILE set, the auto-loaded override is not part of the stack"
}

test_apply_pulls_with_the_operators_override_at_its_install_path() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    assert_eq "$(pull_call)" "compose --project-directory $APP -f $TEST_TMP/stage/docker-compose.yml -f $APP/docker-compose.override.yml pull"
}

test_apply_keeps_an_operator_owned_compose_file_at_its_install_path() {
    make_clone
    fake_host
    printf 'services: {}\n' > "$APP/my-extra.yml"
    printf 'COMPOSE_FILE=./docker-compose.yml:my-extra.yml:%s/docker-compose.local-ocr.yml\n' "$APP" >> "$APP/.env"
    apply_migrate
    assert_status 0
    assert_eq "$(pull_call)" "compose --project-directory $APP -f $TEST_TMP/stage/docker-compose.yml -f $APP/my-extra.yml -f $TEST_TMP/stage/docker-compose.local-ocr.yml pull" \
        "shipped entries come from staging however they are spelled; the operator's stays put"
}

test_migrate_pull_failure_changes_nothing() {
    make_clone
    fake_host
    local before
    before=$(tree_state "$APP")
    touch "$TEST_TMP/fail-pull"
    apply_migrate
    assert_status 1
    assert_contains "$OUT" "could not pull"
    assert_eq "$(tree_state "$APP")" "$before" "a failed pull left the install changed"
    assert_eq "$(env_get APP_VERSION)" v1.1.0
    ! env_has HOST_FILES_VERSION || _fail "marker written after a failed pull"
    assert_dir "$APP/.git"
    compgen -G "$AM_BACKUP_DIR/app_*.tar.gz" >/dev/null || _fail "the backup is the one thing a failed pull leaves"
}

test_migrate_invalid_compose_config_restores_the_clone() {
    make_clone
    fake_host
    local before
    before=$(tree_state "$APP")
    touch "$TEST_TMP/fail-config"
    apply_migrate
    assert_status 1
    assert_contains "$OUT" "do not form a valid stack"
    assert_eq "$(tree_state "$APP")" "$before" "the pruned files were not put back"
    assert_dir "$APP/.git"
    assert_no_file "$APP/.host-files.list"
    assert_not_called "docker compose up"
}

test_a_copy_that_fails_half_way_is_rolled_back() {
    make_clone
    fake_host
    local before
    before=$(tree_state "$APP")
    COPIES=0
    copy_file() {
        COPIES=$((COPIES + 1))
        [ "$COPIES" -lt 4 ] || return 1
        mkdir -p "$(dirname "$2")"
        cp -a --remove-destination "$1" "$2"
    }
    apply_migrate
    assert_status 1
    assert_contains "$OUT" "putting the install back"
    assert_eq "$(tree_state "$APP")" "$before"
    assert_eq "$(env_get APP_VERSION)" v1.1.0
    assert_no_file "$APP/.host-files.list"
}

test_a_failed_migrate_undoes_what_it_created_outside_the_install() {
    # The rehearsal's failure: no cron on the host, so the crontab line cannot be added.
    make_clone
    fake_host
    stub_respond crontab <<'SH'
[ "$1" = - ] && exit 1
exit 1
SH
    local before
    before=$(tree_state "$APP")
    mkdir -p "$APP/.staging"
    make_staging "$APP/.staging/v9.9.9" v9.9.9
    run cmd_apply --app-dir "$APP" --target v9.9.9 --staging "$APP/.staging/v9.9.9" --migrate
    assert_status 1
    assert_contains "$OUT" "putting the install back"
    assert_eq "$(tree_state "$APP")" "$before"
    assert_no_file "$AM_REGISTRY_DIR/default.conf" "the registry entry this run added"
    assert_no_file "$AM_BACKUP_HELPER" "the helper this run added"
    assert_no_dir "$APP/.staging" "the unpacked release"
}

test_a_rollback_never_removes_a_staging_dir_outside_the_install() {
    make_clone
    fake_host
    make_staging "$TEST_TMP/stage" v9.9.9
    touch "$TEST_TMP/fail-config"
    run cmd_apply --app-dir "$APP" --target v9.9.9 --staging "$TEST_TMP/stage" --migrate
    assert_status 1
    assert_dir "$TEST_TMP/stage"
}

test_local_changes_are_saved_and_need_yes() {
    make_clone
    fake_host
    echo "stashed tweak" >> "$APP/deploy/Caddyfile"
    git -C "$APP" stash -q
    echo "# my tweak" >> "$APP/docker-compose.yml"
    mkdir -p "$APP/.staging"
    make_staging "$APP/.staging/v9.9.9" v9.9.9
    run cmd_apply --app-dir "$APP" --target v9.9.9 --staging "$APP/.staging/v9.9.9" --migrate
    assert_status 1
    assert_contains "$OUT" "--yes"
    assert_no_dir "$APP/.staging" "a refusal leaves the unpacked release behind"
    local dir
    dir=$(compgen -G "$AM_BACKUP_DIR/migrate_*")
    assert_file "$dir/uncommitted.patch"
    assert_contains "$(cat "$dir/uncommitted.patch")" "# my tweak"
    assert_file "$dir/stash-0.patch"
    assert_contains "$(cat "$dir/stash-0.patch")" "stashed tweak"
    assert_contains "$(cat "$APP/docker-compose.yml")" "# my tweak" "nothing changed without --yes"
    assert_dir "$APP/.git"

    apply_migrate --yes
    assert_status 0
    assert_not_contains "$(cat "$APP/docker-compose.yml")" "# my tweak"
    assert_file "$dir/uncommitted.patch" "the patches outlive the migration"
}

test_local_commits_are_exported() {
    make_clone
    fake_host
    echo "# committed tweak" >> "$APP/docker-compose.yml"
    git -C "$APP" -c user.email=t@t -c user.name=t commit -qam "local tweak"
    apply_migrate --yes
    assert_status 0
    local patch
    patch=$(compgen -G "$AM_BACKUP_DIR/migrate_*/local-commits/*.patch")
    assert_contains "$(cat "$patch")" "committed tweak"
}

test_a_build_override_is_refused_even_with_yes() {
    make_clone
    fake_host
    printf 'services:\n  alliance-manager:\n    build: .\n' > "$APP/docker-compose.override.yml"
    apply_migrate --yes
    assert_status 1
    assert_contains "$OUT" "docker-compose.override.yml"
    assert_dir "$APP/templates"
}

test_a_tracked_override_is_inspected_and_never_pruned() {
    make_clone
    fake_host
    printf 'services:\n  alliance-manager:\n    volumes:\n      - ./static:/app/static\n' > "$APP/docker-compose.override.yml"
    git -C "$APP" add -f docker-compose.override.yml
    git -C "$APP" -c user.email=t@t -c user.name=t commit -qm "track the override"
    apply_migrate --yes
    assert_status 1
    assert_contains "$OUT" "./static"
    printf 'services:\n  alliance-manager:\n    environment:\n      - EXTRA=1\n' > "$APP/docker-compose.override.yml"
    git -C "$APP" -c user.email=t@t -c user.name=t commit -qam "harmless override"
    apply_migrate --yes
    assert_status 0
    assert_file "$APP/docker-compose.override.yml" "an operator's override is never pruned"
}

test_a_bare_metal_install_is_refused() {
    make_clone
    fake_host
    export AM_LEGACY_DIR="$TEST_TMP/var-lib-lastwar"
    mkdir -p "$AM_LEGACY_DIR"
    echo old > "$AM_LEGACY_DIR/alliance.db"
    rm "$APP/data/alliance.db"
    apply_migrate --yes
    assert_status 1
    assert_contains "$OUT" "v1.1.0"
    assert_dir "$APP/templates"
}

test_trusted_proxy_count_only_under_production() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    assert_eq "$(env_get TRUSTED_PROXY_COUNT)" 1

    rm -rf "$TEST_TMP/app" "$TEST_TMP/origin.git" "$AM_REGISTRY_DIR"
    make_clone
    env_set PRODUCTION false
    apply_migrate
    assert_status 0
    ! env_has TRUSTED_PROXY_COUNT || _fail "TRUSTED_PROXY_COUNT written on a PRODUCTION=false install"
}

test_env_gains_missing_defaults_without_prompting() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    assert_eq "$(env_get OCR_BACKEND_MODE)" cloud
    env_has OCR_ARCHIVE_DIR || _fail "OCR_ARCHIVE_DIR not written"
    env_has CREDENTIAL_ENCRYPTION_KEY || _fail "CREDENTIAL_ENCRYPTION_KEY not written"
    assert_eq "$(env_get TRUSTED_ORIGINS)" "app.warmic.test, localhost:8080" "an unquoted value with spaces survives"
}

test_an_scp_copy_is_overlaid_not_pruned() {
    make_clone
    fake_host
    rm -rf "$APP/.git"
    apply_migrate
    assert_status 0
    assert_file "$APP/templates/x.html" "no history, so nothing is known to be ours to remove"
    assert_no_file "$APP/update.sh" "the root shim is removed by name"
    assert_contains "$OUT" "nothing was removed"
    assert_eq "$(env_get HOST_FILES_VERSION)" v9.9.9
}

test_apply_writes_nothing_outside_the_install() {
    make_clone
    fake_host
    make_staging "$TEST_TMP/stage" v9.9.9
    mkdir -p "$TEST_TMP/elsewhere"
    local stage_before
    stage_before=$(tree_state "$TEST_TMP/stage")
    (cd "$TEST_TMP/elsewhere" && run cmd_apply --app-dir "$APP" --target v9.9.9 --staging "$TEST_TMP/stage" --migrate
     assert_status 0)
    # The staging tree is read, never written — except its removal at the very end.
    [ ! -e "$TEST_TMP/stage" ] || assert_eq "$(tree_state "$TEST_TMP/stage")" "$stage_before"
    assert_eq "$(find "$TEST_TMP/elsewhere" -mindepth 1 | wc -l)" 0 "apply wrote into its working directory"
}

test_apply_ignores_arguments_it_does_not_know() {
    make_clone
    fake_host
    make_staging "$TEST_TMP/stage" v9.9.9
    run cmd_apply --app-dir "$APP" --target v9.9.9 --staging "$TEST_TMP/stage" --migrate --from-the-future x
    assert_status 0
    assert_contains "$OUT" "ignoring an argument"
}

test_the_backup_holds_uploads_but_not_data() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    local tarball
    tarball=$(compgen -G "$AM_BACKUP_DIR/app_*.tar.gz")
    tar -tzf "$tarball" | grep -q '^\./uploads/doc.pdf$' || _fail "uploads/ missing from the backup"
    ! tar -tzf "$tarball" | grep -q '^\./data' || _fail "data/ in the tar backup"
    ! tar -tzf "$tarball" | grep -q '^\./\.git/' || _fail ".git in the tar backup"
    compgen -G "$AM_BACKUP_DIR/db_*.db" >/dev/null || _fail "no database backup"
}

# --- Stage 2: update -------------------------------------------------------------------------

test_update_removes_what_the_new_release_stopped_shipping() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    echo "operator file" > "$APP/scripts/my-helper.sh"
    rm -rf "$TEST_TMP/stage"
    make_staging "$TEST_TMP/stage" v9.9.10 deploy/docker-compose.override.yml.example
    run cmd_apply --app-dir "$APP" --target v9.9.10 --staging "$TEST_TMP/stage"
    assert_status 0
    assert_no_file "$APP/deploy/docker-compose.override.yml.example"
    assert_file "$APP/deploy/Caddyfile"
    assert_file "$APP/scripts/my-helper.sh" "a file no release shipped is never a candidate"
    assert_contains "$OUT" "removed 1 file"
    assert_eq "$(env_get HOST_FILES_VERSION)" v9.9.10
}

test_update_config_failure_restores_the_previous_release() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    local before
    before=$(tree_state "$APP")
    touch "$TEST_TMP/fail-config"
    rm -rf "$TEST_TMP/stage"
    make_staging "$TEST_TMP/stage" v9.9.10 deploy/docker-compose.override.yml.example
    echo "# changed in 9.9.10" >> "$TEST_TMP/stage/docker-compose.yml"
    run cmd_apply --app-dir "$APP" --target v9.9.10 --staging "$TEST_TMP/stage"
    assert_status 1
    assert_eq "$(tree_state "$APP")" "$before"
    assert_eq "$(env_get HOST_FILES_VERSION)" v9.9.9
}

test_a_failed_up_keeps_the_marker_for_a_retry() {
    make_clone
    fake_host
    touch "$TEST_TMP/fail-up"
    apply_migrate
    assert_status 1
    assert_contains "$OUT" "did not start"
    assert_eq "$(env_get HOST_FILES_VERSION)" v9.9.9 "the install is consistent; only up failed"
}

# --- Caddy -----------------------------------------------------------------------------------

deployed_caddyfile() {  # deployed_caddyfile CONTENT
    mkdir -p "$(dirname "$AM_CADDYFILE")"
    printf '%s\n' "$1" > "$AM_CADDYFILE"
}

test_caddyfile_rev0_is_rerendered_with_a_backup() {
    make_clone
    fake_host
    deployed_caddyfile 'app.warmic.test {
    reverse_proxy localhost:8080
}'
    apply_migrate
    assert_status 0
    assert_eq "$(caddyfile_rev "$AM_CADDYFILE")" 1
    assert_contains "$(cat "$AM_CADDYFILE")" "collabora.warmic.test {"
    compgen -G "$AM_CADDYFILE.backup_*" >/dev/null || _fail "no dated backup of the old Caddyfile"
    assert_called "sudo caddy validate --adapter caddyfile --config"
    assert_called "sudo systemctl reload caddy"
}

test_caddyfile_that_fails_validation_is_left_alone() {
    make_clone
    fake_host
    deployed_caddyfile 'app.warmic.test { }'
    local before
    before=$(cat "$AM_CADDYFILE")
    touch "$TEST_TMP/fail-validate"
    apply_migrate
    assert_status 0
    assert_eq "$(cat "$AM_CADDYFILE")" "$before"
    assert_not_called "sudo systemctl reload caddy"
    assert_contains "$OUT" "did not validate"
}

test_caddyfile_at_the_same_rev_is_untouched() {
    make_clone
    fake_host
    deployed_caddyfile '# alliance-manager caddyfile rev 1
my hand edit'
    apply_migrate
    assert_status 0
    assert_contains "$(cat "$AM_CADDYFILE")" "my hand edit"
    assert_not_called "sudo caddy validate"
}

test_caddyfile_not_rendered_from_a_placeholder_domain() {
    make_clone
    fake_host
    env_set APP_DOMAIN app.example.com
    deployed_caddyfile 'old'
    apply_migrate
    assert_status 0
    assert_eq "$(cat "$AM_CADDYFILE")" old
    assert_contains "$OUT" "set APP_DOMAIN"
}

# --- backup ----------------------------------------------------------------------------------

test_backup_keeps_ten_of_each_and_leaves_nightlies_alone() {
    make_clone
    fake_host
    mkdir -p "$AM_BACKUP_DIR"
    local i
    for i in $(seq 10 21); do
        touch -d "2026-01-$i" "$AM_BACKUP_DIR/app_202601${i}_000000.tar.gz" "$AM_BACKUP_DIR/db_202601${i}_000000.db"
    done
    touch -d "2020-01-01" "$AM_BACKUP_DIR/nightly_default_old.db"
    run cmd_backup
    assert_status 0
    assert_eq "$(find "$AM_BACKUP_DIR" -name 'app_*' | wc -l)" 10
    assert_eq "$(find "$AM_BACKUP_DIR" -name 'db_*' | wc -l)" 10
    assert_no_file "$AM_BACKUP_DIR/app_20260110_000000.tar.gz" "the oldest goes first"
    assert_file "$AM_BACKUP_DIR/nightly_default_old.db"
}

test_backup_without_wal_or_shm() {
    make_clone
    fake_host
    run cmd_backup
    assert_status 0
    touch "$APP/data/alliance.db-wal"
    run cmd_backup
    assert_status 0
    assert_called "sudo chown --reference=$APP/data/alliance.db $APP/data/alliance.db-wal"
}

# --- status ----------------------------------------------------------------------------------

test_status_shows_the_containers_health() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    stub_respond docker <<'SH'
case "$*" in
    *Config.Image*) echo ghcr.io/shodiwarmic/lastwar-alliance-manager:v9.9.9 ;;
    *State.Health*) echo healthy ;;
esac
exit 0
SH
    run cmd_status
    assert_status 0
    assert_contains "$OUT" "Health:                    healthy"
}

test_status_says_when_the_container_is_not_running() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    stub_respond docker <<'SH'
case "$*" in inspect*) exit 1 ;; esac
exit 0
SH
    run cmd_status
    assert_status 0
    assert_contains "$OUT" "(container not running)"
    assert_not_contains "$OUT" "Health:"
}

# --- Driven unattended: the host update agent's hooks ------------------------------------------

# migrated_install — a v9.9.9 install, migrated, with an empty stub log.
migrated_install() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    : > "$STUB_LOG"
}

# apply_update [extra args] — stage 2 of an update to v9.9.10.
apply_update() {
    rm -rf "$TEST_TMP/stage"
    make_staging "$TEST_TMP/stage" v9.9.10
    run cmd_apply --app-dir "$APP" --target v9.9.10 --staging "$TEST_TMP/stage" "$@"
}

# as_root — run the scripts as the agent does: euid 0, under AM_ALLOW_ROOT, with $APP owned by
# someone else. The tests cannot own a file as root, so the uid "root's" files carry is this
# user's, and chown is logged (then really run, as the no-op it is here).
as_root() {
    pf_euid() { echo 0; }
    am_stat_uid() { echo 4242; }
    AM_ROOT_UID=$(id -u)
    export AM_ALLOW_ROOT=1 AM_ROOT_UID
    ln -sf "$TESTS_DIR/lib/stubs/stub" "$STUB_DIR/bin/chown"
    stub_respond chown <<'SH'
exec /usr/bin/chown "$@"
SH
}

# chowned — the paths ownership repair handed back, relative to $APP, one per line.
chowned() {
    stub_calls chown | grep -- "-h --reference=$APP " | tr ' ' '\n' | grep "^$APP/" | sed "s|^$APP/||" | sort -u
}

receipt_dir() { mkdir -p "$AM_STATE_DIR/default"; printf '%s' "$AM_STATE_DIR/default"; }

test_root_is_refused_without_the_agents_exemption() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    pf_euid() { echo 0; }
    run cmd_update --asset "$(make_asset v9.9.10)"
    assert_status 1
    assert_contains "$OUT" "running as root"
}

test_the_agents_exemption_passes_preflight_and_reaches_the_handover() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    as_root
    am_exec() { printf '%s\n' "$@" > "$TEST_TMP/exec-args"; }
    run cmd_update --asset "$(make_asset v9.9.10)" --yes --receipt "$(receipt_dir)/receipt-abc-123" --offline
    assert_status 0
    assert_eq "$(sed -n '2,$p' "$TEST_TMP/exec-args" | tr '\n' ' ')" \
        "apply --app-dir $APP --target v9.9.10 --staging $APP/.staging/v9.9.10 --yes --receipt $AM_STATE_DIR/default/receipt-abc-123 --offline " \
        "stage 1 forwards --receipt and --offline"
}

test_stage1_refuses_offline_without_an_asset() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    run cmd_update --version v9.9.10 --offline
    assert_status 1
    assert_contains "$OUT" "--offline needs --asset"
}

test_stage1_refuses_a_receipt_that_names_no_id() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    run cmd_update --asset "$(make_asset v9.9.10)" --receipt "$TEST_TMP/receipt-../../etc"
    assert_status 1
    assert_contains "$OUT" "receipt-<id>"
}

test_stage1_takes_the_lock_and_refuses_while_it_is_held() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    local asset
    asset=$(make_asset v9.9.10)
    exec 8>>"$APP/.update.lock"
    flock -n 8 || _fail "the test could not take the lock"
    am_exec() { _fail "no handover while another updater holds the lock"; }
    run cmd_update --asset "$asset"
    assert_status 1
    assert_contains "$OUT" "another update of this install is running"
    assert_no_dir "$APP/.staging" "nothing staged"
    exec 8>&-

    am_exec() { echo handed-over; }
    run cmd_update --asset "$asset"
    assert_status 0
    assert_contains "$OUT" handed-over
    assert_file "$APP/.update.lock" "the lock file is never deleted"
}

test_a_held_lock_is_reported_before_anything_reads_env() {
    # Mid-update under the agent, .env can be briefly root's and unreadable: the operator must
    # be told an update is running, not that the install is unmigrated.
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    exec 8>>"$APP/.update.lock"
    flock -n 8 || _fail "the test could not take the lock"
    chmod 000 "$APP/.env"
    run cmd_update --version v9.9.10
    chmod 600 "$APP/.env"
    exec 8>&-
    assert_status 1
    assert_contains "$OUT" "another update of this install is running"
    assert_not_contains "$OUT" "not been migrated"
}

test_stage1_reuses_the_lock_the_agent_hands_down() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    local asset
    asset=$(make_asset v9.9.10)
    exec 9>>"$APP/.update.lock"
    flock -n 9 || _fail "the test could not take the lock"
    am_exec() { echo handed-over; }
    AM_UPDATE_LOCK_FD=9 run cmd_update --asset "$asset"
    assert_status 0
    assert_contains "$OUT" handed-over
    exec 9>&-
}

test_a_stage1_failure_as_root_leaves_nothing_root_owned() {
    make_clone
    fake_host
    printf 'HOST_FILES_VERSION=v9.9.9\n' >> "$APP/.env"
    as_root
    stub_respond curl <<'SH'
exit 22
SH
    run cmd_update --version v9.9.10 --yes
    assert_status 1
    assert_contains "$OUT" "could not download"
    chowned | grep -qx '.update.lock' || _fail "the lock stage 1 created as root was not handed back: $(chowned | tr '\n' ' ')"
    chowned | grep -qx '.env' || _fail ".env not covered"
}

test_ownership_repair_covers_the_install_and_skips_data_uploads_and_git() {
    migrated_install
    mkdir -p "$APP/.git" && touch "$APP/.git/HEAD"
    as_root
    apply_update
    assert_status 0
    local paths
    paths=$(chowned)
    grep -qx '.env' <<<"$paths" || _fail ".env not handed back: $paths"
    grep -qx 'scripts/manage.sh' <<<"$paths" || _fail "the new scripts not handed back"
    ! grep -qE '^(data|uploads|\.git)(/|$)' <<<"$paths" || _fail "walked the container's data or .git: $paths"
}

test_ownership_repair_runs_on_a_rollback_and_after_a_failed_up() {
    migrated_install
    as_root
    touch "$TEST_TMP/fail-pull"
    apply_update
    assert_status 1
    assert_contains "$OUT" "putting the install back"
    chowned | grep -qx '.env' || _fail "a rollback left .env root-owned"
    rm "$TEST_TMP/fail-pull"

    : > "$STUB_LOG"
    touch "$TEST_TMP/fail-up"
    apply_update
    assert_status 1
    assert_contains "$OUT" "did not start"
    chowned | grep -qx '.env' || _fail "a failed up after the marker left .env root-owned"
}

test_ownership_repair_reaches_an_install_under_a_directory_named_data() {
    APP="$TEST_TMP/data/am"
    mkdir -p "$APP"
    make_staging "$APP" v9.9.9
    mkdir -p "$APP/data" "$APP/uploads"
    printf 'SESSION_KEY=x\nAPP_VERSION=v9.9.9\nHOST_FILES_VERSION=v9.9.9\nAPP_DOMAIN=app.warmic.test\nCOLLABORA_DOMAIN=c.warmic.test\n' > "$APP/.env"
    echo db > "$APP/data/alliance.db"
    ENV_FILE="$APP/.env"
    fake_host
    as_root
    apply_update
    assert_status 0
    chowned | grep -qx '.env' || _fail "an install under .../data/ was skipped: $(chowned | tr '\n' ' ')"
    ! chowned | grep -q '^data/' || _fail "its own data/ was walked"
}

test_ownership_repair_does_nothing_unless_root() {
    migrated_install
    ln -sf "$TESTS_DIR/lib/stubs/stub" "$STUB_DIR/bin/chown"
    apply_update
    assert_status 0
    assert_eq "$(stub_calls chown | grep -c -- '--reference=' || true)" 0
}

# receipt_get FILE KEY
receipt_get() { sed -n "s/^$2=//p" "$1"; }

test_the_receipt_names_this_runs_backups_and_its_schema() {
    migrated_install
    echo 82 > "$APP/data/alliance.db.schema"
    local r
    r="$(receipt_dir)/receipt-req-1"
    apply_update --receipt "$r"
    assert_status 0
    assert_file "$r"
    assert_eq "$(head -n1 "$r")" "alliance-manager update receipt 1"
    assert_eq "$(receipt_get "$r" id)" req-1
    assert_eq "$(receipt_get "$r" app_dir)" "$APP"
    assert_eq "$(receipt_get "$r" from)" v9.9.9
    assert_eq "$(receipt_get "$r" to)" v9.9.10
    assert_eq "$(receipt_get "$r" schema_max)" 82
    assert_eq "$(receipt_get "$r" phase)" up
    local db
    db=$(receipt_get "$r" db_backup)
    [[ $db == "$AM_BACKUP_DIR"/db_*-req-1.db ]] || _fail "the db backup is not named for the request: $db"
    [[ $(receipt_get "$r" app_backup) == "$AM_BACKUP_DIR"/app_*-req-1.tar.gz ]] || _fail "app backup name"
    assert_eq "$(receipt_get "$r" pinned_db)" "$AM_BACKUP_DIR/pinned/req-1.db"
    [ "$db" -ef "$AM_BACKUP_DIR/pinned/req-1.db" ] || _fail "pinned/req-1.db is not a hardlink of the backup"
    assert_no_file "$r".* "no temp file left beside the receipt"
}

test_the_pin_exists_before_the_pull_and_a_later_backup_leaves_it_alone() {
    migrated_install
    echo "before the update" > "$APP/data/alliance.db"
    stub_respond docker <<'SH'
case "$*" in
    *"config --format json")
        printf '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"%s/data","target":"/app/data"}]}}}\n' "$PWD" ;;
    "compose "*pull)
        if [ -f "$AM_BACKUP_DIR/pinned/req-2.db" ]; then echo "# pinned before pull" >> "$STUB_LOG"; fi ;;
esac
exit 0
SH
    apply_update --receipt "$(receipt_dir)/receipt-req-2"
    assert_status 0
    grep -q '^# pinned before pull$' "$STUB_LOG" || _fail "the database pin did not exist when the pull ran"
    echo "after the update" > "$APP/data/alliance.db"
    run cmd_backup
    assert_status 0
    assert_eq "$(cat "$AM_BACKUP_DIR/pinned/req-2.db")" "before the update" "a later backup changed the restore point"
    assert_eq "$(find "$AM_BACKUP_DIR" -maxdepth 1 -name 'db_*' | wc -l)" 3 "the migrate's, the update's and the later backup's"
}

test_the_receipt_records_how_far_apply_got() {
    migrated_install
    local r
    r="$(receipt_dir)/receipt-req-3"
    # Record each phase as the docker stub sees it: pull (armed), up (marker).
    stub_respond docker <<SH
case "\$*" in
    *"config --format json")
        printf '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"%s/data","target":"/app/data"}]}}}\n' "\$PWD" ;;
    "compose "*pull) echo "# pull phase=\$(sed -n 's/^phase=//p' "$r")" >> "\$STUB_LOG" ;;
    "compose up -d") echo "# up phase=\$(sed -n 's/^phase=//p' "$r")" >> "\$STUB_LOG"; [ -f "$TEST_TMP/fail-up" ] && exit 1 ;;
esac
exit 0
SH
    apply_update --receipt "$r"
    assert_status 0
    grep -q '^# pull phase=armed$' "$STUB_LOG" || _fail "phase at the pull: $(grep '^# pull' "$STUB_LOG")"
    grep -q '^# up phase=marker$' "$STUB_LOG" || _fail "phase at up: $(grep '^# up' "$STUB_LOG")"
    assert_eq "$(receipt_get "$r" phase)" up

    touch "$TEST_TMP/fail-up"
    r="$(receipt_dir)/receipt-req-3"
    apply_update --receipt "$r"
    assert_status 1
    assert_eq "$(receipt_get "$r" phase)" marker "a failed up stops at marker"
}

test_offline_refuses_a_missing_image_and_changes_nothing() {
    migrated_install
    local before
    before=$(tree_state "$APP")
    printf 'collabora/code@sha256:feed\n' > "$TEST_TMP/absent-images"
    apply_update --offline
    assert_status 1
    assert_contains "$OUT" "offline, and not on this host: collabora/code@sha256:feed"
    assert_eq "$(tree_state "$APP")" "$before"
    assert_not_called "docker compose .* pull"
    ! stub_calls docker | grep -q ' pull$' || _fail "offline pulled"
}

test_offline_with_every_image_present_pulls_nothing_and_never_pulls_on_up() {
    migrated_install
    apply_update --offline
    assert_status 0
    ! stub_calls docker | grep -q ' pull$' || _fail "offline pulled: $(stub_calls docker | tr '\n' '|')"
    stub_calls docker | grep -qx 'compose up -d --pull never' || _fail "up should be --pull never: $(stub_calls docker | tr '\n' '|')"
    stub_calls docker | grep -qx 'image inspect ghcr.io/shodiwarmic/lastwar-alliance-manager:v9.9.10' \
        || _fail "the target app image was not checked"
}

test_no_prune_under_the_agent() {
    migrated_install
    exec 9>>"$APP/.update.lock"
    AM_UPDATE_LOCK_FD=9 apply_update
    assert_status 0
    ! stub_calls docker | grep -q '^image prune' || _fail "pruned under the agent"
    exec 9>&-
    apply_update
    stub_calls docker | grep -q '^image prune' || _fail "a terminal update should still prune"
}

test_a_stack_that_moves_the_data_is_refused_before_backup_or_pull() {
    migrated_install
    local before
    before=$(tree_state "$APP")
    for mounts in \
        '{"services":{"alliance-manager":{"volumes":[{"type":"volume","source":"amdata","target":"/app/data"}]}}}' \
        '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"'"$APP"'/data","target":"/app/data"},{"type":"bind","source":"/srv/other.db","target":"/app/data/alliance.db"}]}}}' \
        '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"'"$APP"'/data","target":"/app/data"},{"type":"volume","source":"spool","target":"/app/data/update"}]}}}' \
        '{"services":{"alliance-manager":{}}}'; do
        printf '%s\n' "$mounts" > "$TEST_TMP/mounts.json"
        : > "$STUB_LOG"
        apply_update
        assert_status 1
        assert_contains "$OUT" "would not keep the app's data in $APP/data"
        assert_not_called "sudo tar" "a backup was taken"
        ! stub_calls docker | grep -q ' pull$' || _fail "pulled"
        assert_eq "$(tree_state "$APP")" "$before"
    done
}

test_apply_from_outside_the_install_acts_on_it() {
    # The agent's recovery shape: a previous release's apply, unpacked in its state directory,
    # driven with --app-dir and --staging pointing at that same directory.
    migrated_install
    local rec="$AM_STATE_DIR/default/recovery-req-4"
    mkdir -p "$rec"
    make_staging "$rec" v9.9.10
    (cd "$TEST_TMP" && run "$rec/scripts/manage.sh" apply --app-dir "$APP" --target v9.9.10 --staging "$rec" --offline --yes
     assert_status 0)
    assert_eq "$(ENV_FILE="$APP/.env" env_get HOST_FILES_VERSION)" v9.9.10
    assert_dir "$rec" "remove_staging touches only \$APP_DIR/.staging/"
}

test_apply_installs_the_update_agent_and_a_failed_apply_takes_it_back() {
    migrated_install
    assert_file "$AM_UPDATE_AGENT" "the migrate installed the agent"
    local before
    before=$(cat "$AM_UPDATE_AGENT")
    # A failure after the agent was replaced (step 9b) but before the marker: the config check.
    stub_respond docker <<'SH'
case "$*" in
    *"config --format json")
        printf '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"%s/data","target":"/app/data"}]}}}\n' "$PWD" ;;
    "compose config -q") exit 1 ;;
esac
exit 0
SH
    apply_update
    assert_status 1
    assert_eq "$(cat "$AM_UPDATE_AGENT")" "$before" "the v9.9.10 agent survived a failed apply"
    stub_respond docker <<'SH'
case "$*" in
    *"config --format json")
        printf '{"services":{"alliance-manager":{"volumes":[{"type":"bind","source":"%s/data","target":"/app/data"}]}}}\n' "$PWD" ;;
esac
exit 0
SH
    apply_update
    assert_status 0
    assert_contains "$(cat "$AM_UPDATE_AGENT")" "AGENT_RELEASE='v9.9.10'"
    assert_contains "$OUT" "Host update agent: replaced"
}
