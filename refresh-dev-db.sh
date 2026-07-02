#!/usr/bin/env bash
#
# refresh-dev-db.sh — Pull the PRODUCTION database + uploaded files into this TEST/dev
# environment, while preserving dev's local secrets.
#
# What it does:
#   1. Online-snapshots the prod SQLite DB over SSH (no prod downtime) and pulls it down.
#   2. (optional) Tars prod ./uploads and ./data/ocr-archive and pulls them down.
#   3. Stops the dev container.
#   4. Backs up dev's `credentials` table (AES-GCM encrypted external API keys).
#   5. Replaces dev's DB with the prod snapshot; restores dev's `credentials` over prod's.
#   6. Mirrors the prod files into dev (so the DB's file references resolve).
#   7. Starts dev (the app runs `goose up` migrations on boot).
#
# Secrets model:
#   - .env secrets (CREDENTIAL_ENCRYPTION_KEY, SESSION_KEY) are NOT in the DB, so a DB-file
#     swap preserves them for free.
#   - The only DB-resident secret is the `credentials` table. Its blobs are encrypted with
#     CREDENTIAL_ENCRYPTION_KEY; dev and prod typically use different keys, so prod's blobs
#     won't decrypt in dev. Hence we restore dev's own `credentials` rows after the swap.
#
# Files model:
#   - `credentials` is preserved from dev. `./uploads` (Files feature) and `./data/ocr-archive`
#     are the opposite: the swapped DB references PROD's files, so we pull prod's to match.
#
# Requirements: docker + ssh + scp locally; docker on the prod host. All SQLite/tar/file work
# runs inside a throwaway alpine container as root, so neither host needs sqlite3 installed and
# the uid-1001-owned ./data and ./uploads dirs stay writable.
#
# SSH auth: uses your system ssh, so anything ssh supports works — ssh-agent, a passphrase-
# protected key, or password-only auth (and ~/.ssh/config: User/Port/IdentityFile/ProxyJump…).
# Connection multiplexing means you're prompted for a password/passphrase at most once per run.
# (Requires an OpenSSH client with ControlMaster support — standard on Linux/macOS/WSL.)
#
# NOTE: TEST-env convenience only. It copies real production member data + files into dev, and
# is intentionally destructive to the local dev DB/uploads (no backup — re-pull to redo).

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"

# ─── Config ─────────────────────────────────────────────────────────────────────
# Local values live in refresh-dev-db.conf (gitignored). Copy refresh-dev-db.conf.example
# to refresh-dev-db.conf and edit it. This script (the logic) stays version-controlled.
# A value set in the .conf — or exported in the environment — overrides the default below.
# shellcheck disable=SC1091
[ -f "$HERE/refresh-dev-db.conf" ] && . "$HERE/refresh-dev-db.conf"

PROD_SSH="${PROD_SSH:-}"                          # REQUIRED: ssh target, e.g. user@prod-host
PROD_DIR="${PROD_DIR:-}"                          # REQUIRED: compose project dir on prod (has ./data, ./uploads)
DEV_DIR="${DEV_DIR:-$HERE}"                       # this repo (default: script's dir)
DB_NAME="${DB_NAME:-alliance.db}"                # DB filename inside ./data (matches DATABASE_PATH)
DEV_SERVICE="${DEV_SERVICE:-alliance-manager}"   # compose service to stop/start
APP_UID="${APP_UID:-1001}"                        # in-container app user that must own the files
IMG="${IMG:-alpine:latest}"                       # throwaway image for sqlite/tar/file work
SYNC_UPLOADS="${SYNC_UPLOADS:-1}"                 # pull ./uploads from prod (Files feature) — usually yes
SYNC_OCR_ARCHIVE="${SYNC_OCR_ARCHIVE:-0}"        # pull ./data/ocr-archive (bulky audit data) — usually no
# ────────────────────────────────────────────────────────────────────────────────

log() { printf '\033[1;36m▶ %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

[ -n "$PROD_SSH" ] && [ -n "$PROD_DIR" ] || \
  die "Set PROD_SSH and PROD_DIR — copy refresh-dev-db.conf.example to refresh-dev-db.conf and edit it."

printf '\033[1;33mThis REPLACES the local dev DB%s with production data.\033[0m\n' \
  "$([ "$SYNC_UPLOADS" = 1 ] && echo ' + uploaded files')"
read -r -p "Continue? [y/N] " ans; [ "$ans" = "y" ] || die "Aborted."

WORK="$(mktemp -d)"
CTL="$WORK/cm.sock"   # SSH connection-multiplexing socket
trap 'ssh -o ControlPath="$CTL" -O exit "$PROD_SSH" 2>/dev/null || true; rm -rf "$WORK"' EXIT

# Reuse ONE authenticated SSH connection for every ssh/scp below, so whatever auth your prod
# uses — ssh-agent, a passphrase-protected key, or password auth — is prompted for only once
# (instead of once per connection). Honors ~/.ssh/config (User/Port/IdentityFile/ProxyJump/…).
SSH=(ssh -o ControlMaster=auto -o ControlPath="$CTL" -o ControlPersist=120)
SCP=(scp -q -o ControlMaster=auto -o ControlPath="$CTL" -o ControlPersist=120)

log "Connecting to $PROD_SSH (you'll be prompted once for any password/passphrase)…"
"${SSH[@]}" "$PROD_SSH" "test -d '$PROD_DIR'" \
  || die "Can't reach $PROD_SSH, or PROD_DIR ($PROD_DIR) doesn't exist there."

# Produce an artifact under /out (=/tmp on prod) inside a root container on prod, scp it into
# $WORK, then remove the remote copy.  $1 = remote sh snippet, $2 = artifact filename.
pull_from_prod() {
  "${SSH[@]}" "$PROD_SSH" "cd '$PROD_DIR' && docker run --rm -v \"\$PWD\":/proj:ro -v /tmp:/out '$IMG' sh -c '$1'"
  "${SCP[@]}" "$PROD_SSH:/tmp/$2" "$WORK/$2"
  # The artifact is created root-owned by the container above; /tmp's sticky bit means the
  # non-root SSH login user can't rm it. Delete it via a root container, same as it was made.
  "${SSH[@]}" "$PROD_SSH" "docker run --rm -v /tmp:/out '$IMG' rm -f /out/$2"
}

# Wipe a dev dir and extract a $WORK tarball into it, as root, chowned to the app user.
# $1 = dev subpath (e.g. uploads), $2 = tarball name in $WORK.
replace_dev_dir() {
  [ -s "$WORK/$2" ] || die "Refusing to wipe dev/$1 — $2 missing or empty."
  docker run --rm -v "$DEV_DIR/$1":/d -v "$WORK":/work:ro "$IMG" sh -c '
    set -e
    find /d -mindepth 1 -delete
    tar xzf /work/'"$2"' -C /d
    chown -R '"$APP_UID"':'"$APP_UID"' /d
  '
}

# ─── 1. Prod DB → consistent online snapshot ────────────────────────────────────
log "Snapshotting prod DB (online .backup — no prod downtime)…"
pull_from_prod "apk add -q --no-cache sqlite && sqlite3 /proj/data/$DB_NAME \".backup /out/prod_snapshot.db\"" prod_snapshot.db
[ -s "$WORK/prod_snapshot.db" ] || die "DB snapshot empty — aborting before touching dev."

# ─── 2. Prod files (optional) ───────────────────────────────────────────────────
[ "$SYNC_UPLOADS" = 1 ]     && { log "Tarring prod ./uploads…";          pull_from_prod "tar czf /out/uploads.tgz -C /proj/uploads ." uploads.tgz; }
[ "$SYNC_OCR_ARCHIVE" = 1 ] && { log "Tarring prod ./data/ocr-archive…"; pull_from_prod "tar czf /out/ocrarch.tgz -C /proj/data/ocr-archive ." ocrarch.tgz; }

# ─── 3. Stop dev (never swap a DB file under a running SQLite) ───────────────────
log "Stopping dev service '$DEV_SERVICE'…"
( cd "$DEV_DIR" && docker compose stop "$DEV_SERVICE" )

# ─── 4+5. Back up dev credentials, swap DB, restore credentials (root, via /work) ─
log "Backing up dev credentials, swapping DB, restoring credentials…"
docker run --rm -v "$DEV_DIR/data":/data -v "$WORK":/work "$IMG" sh -c '
  set -e
  apk add -q --no-cache sqlite
  sqlite3 /data/'"$DB_NAME"' ".dump credentials" > /work/dev_credentials.sql   # BLOBs as X'"'"'..'"'"' hex
  rm -f /data/'"$DB_NAME"' /data/'"$DB_NAME"'-wal /data/'"$DB_NAME"'-shm
  cp /work/prod_snapshot.db /data/'"$DB_NAME"'
  sqlite3 /data/'"$DB_NAME"' "DROP TABLE IF EXISTS credentials;"
  sqlite3 /data/'"$DB_NAME"' < /work/dev_credentials.sql
  chown '"$APP_UID"':'"$APP_UID"' /data/'"$DB_NAME"'
'

# ─── 6. Mirror prod files into dev ──────────────────────────────────────────────
[ "$SYNC_UPLOADS" = 1 ]     && { log "Replacing dev ./uploads…";          replace_dev_dir uploads uploads.tgz; }
[ "$SYNC_OCR_ARCHIVE" = 1 ] && { log "Replacing dev ./data/ocr-archive…"; replace_dev_dir data/ocr-archive ocrarch.tgz; }

# ─── 7. Start dev — migrations (goose up) run on boot ───────────────────────────
log "Starting dev service…"
( cd "$DEV_DIR" && docker compose up -d "$DEV_SERVICE" )

log "Recent migration / startup lines:"
( cd "$DEV_DIR" && docker compose logs --since 30s "$DEV_SERVICE" 2>&1 | grep -E 'OK   [0-9]|Server listening|ERROR' || true )

cat <<EOF

✓ Dev refreshed from prod. Preserved: dev .env secrets + dev credentials. Pulled: prod DB$([ "$SYNC_UPLOADS" = 1 ] && echo " + uploads")$([ "$SYNC_OCR_ARCHIVE" = 1 ] && echo " + ocr-archive").
  • OCR/Vision uses dev's restored credentials; add a dev key in Settings if dev had none.
  • goose only migrates up — keep this checkout at/ahead of prod's schema (your normal case).
EOF
