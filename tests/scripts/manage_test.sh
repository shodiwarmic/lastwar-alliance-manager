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
    "compose pull")
        echo "# pull saw APP_VERSION=$(env_val APP_VERSION) marker=$(env_val HOST_FILES_VERSION)" >> "$STUB_LOG"
        [ -f "$TEST_TMP/fail-pull" ] && exit 1 ;;
    "compose config -q")
        [ -f "$TEST_TMP/fail-config" ] && { echo "invalid compose project" >&2; exit 1; } ;;
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
    for t in dirname cat grep sed tr; do ln -s "$(command -v $t)" "$bin/$t"; done
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
}

test_migrate_orders_pin_pull_marker_up() {
    make_clone
    fake_host
    apply_migrate
    assert_status 0
    grep -q '^# pull saw APP_VERSION=v9.9.9 marker=$' "$STUB_LOG" \
        || _fail "the pull must see the new pin and no marker: $(grep '^# pull' "$STUB_LOG")"
    grep -q '^# up saw marker=v9.9.9$' "$STUB_LOG" || _fail "up must see the marker"
    [ "$(line_of 'docker compose pull')" -lt "$(line_of 'docker compose config -q')" ] || _fail "files copied before the pull"
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

test_local_changes_are_saved_and_need_yes() {
    make_clone
    fake_host
    echo "stashed tweak" >> "$APP/deploy/Caddyfile"
    git -C "$APP" stash -q
    echo "# my tweak" >> "$APP/docker-compose.yml"
    apply_migrate
    assert_status 1
    assert_contains "$OUT" "--yes"
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
