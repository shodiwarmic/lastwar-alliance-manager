#!/bin/bash
# shellcheck source-path=SCRIPTDIR
# scripts/install.sh — install Alliance Manager on a fresh Debian or Ubuntu host.
#
# Runs from an unpacked host-files release asset, in whatever directory you chose:
#
#   mkdir -p ~/alliance-manager && cd ~/alliance-manager
#   curl -fsSL -o host-files.tar.gz \
#     https://github.com/shodiwarmic/lastwar-alliance-manager/releases/latest/download/host-files.tar.gz
#   tar -xzf host-files.tar.gz && rm host-files.tar.gz
#   ./scripts/install.sh
#
# That directory becomes the install: .env, data/ and uploads/ are created beside the
# scripts, and it is recorded in /etc/alliance-manager/installs.d/ so the host knows where
# its install lives. Later updates are ./scripts/manage.sh update.
#
# Every prompt has a flag, so the whole thing can run unattended:
#
#   ./scripts/install.sh --non-interactive --domain app.example.org
#
# Options:
#   --domain NAME            the app's domain (required non-interactively)
#   --collabora-domain NAME  the document editor's domain (default: collabora.<domain>)
#   --proxy caddy|none       install and configure Caddy (default), or bring your own proxy
#   --ocr cloud|local        Google Cloud Vision (default) or the local PaddleOCR sidecar
#   --archive                prepare local-disk OCR request archival (local OCR only)
#   --ignore ID[,ID...]      accept a failed prerequisite check (recorded in install.log)
#   --name NAME              the name this install is registered under (default: default)
#   --non-interactive        never prompt; a missing required value is an error
#
# Re-running over the same directory is safe: each step adds what is missing and changes
# nothing an operator has set.
set -eE
# Without this, bash runs every $(...) with `set -e` OFF.
shopt -s inherit_errexit

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib/common.sh
source "$SCRIPT_DIR/lib/common.sh"
# shellcheck source=lib/registry.sh
source "$SCRIPT_DIR/lib/registry.sh"
# shellcheck source=lib/preflight.sh
source "$SCRIPT_DIR/lib/preflight.sh"

usage() {
    sed -n '/^# Options:/,/^# Re-running/p' "${BASH_SOURCE[0]}" | sed '$d; s/^# \{0,1\}//'
}

parse_args() {
    DOMAIN='' COLLAB_DOMAIN='' PROXY=caddy OCR=cloud ARCHIVE=0 IGNORE='' NAME=default
    NON_INTERACTIVE=0
    local explicit_ocr=0 explicit_proxy=0
    while [ $# -gt 0 ]; do
        case $1 in
            --domain) DOMAIN=${2:-}; shift 2 ;;
            --collabora-domain) COLLAB_DOMAIN=${2:-}; shift 2 ;;
            --proxy) PROXY=${2:-}; explicit_proxy=1; shift 2 ;;
            --ocr) OCR=${2:-}; explicit_ocr=1; shift 2 ;;
            --archive) ARCHIVE=1; shift ;;
            --ignore) IGNORE=${2:-}; shift 2 ;;
            --name) NAME=${2:-}; shift 2 ;;
            --non-interactive) NON_INTERACTIVE=1; shift ;;
            -h|--help) usage; exit 0 ;;
            *) die "unknown option: $1 (see --help)" ;;
        esac
    done
    case $PROXY in caddy|none) ;; *) die "--proxy must be caddy or none" ;; esac
    case $OCR in cloud|local) ;; *) die "--ocr must be cloud or local" ;; esac
    registry_valid_name "$NAME" || die "--name may hold letters, digits, . _ and - only"
    ASK_PROXY=$((1 - explicit_proxy))
    ASK_OCR=$((1 - explicit_ocr))
    export NON_INTERACTIVE
}

# ask VAR "prompt" [default] — read a value unless one was given; never prompts when
# non-interactive.
ask() {
    local __var=$1 prompt=$2 default=${3:-} reply
    if [ -n "${!__var}" ]; then
        return 0
    fi
    if [ "$NON_INTERACTIVE" = 1 ]; then
        printf -v "$__var" '%s' "$default"
        return 0
    fi
    if [ -n "$default" ]; then
        read -r -p "$prompt [$default]: " reply
    else
        read -r -p "$prompt: " reply
    fi
    printf -v "$__var" '%s' "${reply:-$default}"
}

gather_choices() {
    step "Configuration"
    ask DOMAIN "The app's domain (e.g. app.example.org)"
    [ -n "$DOMAIN" ] || die "a domain is required (--domain)"
    if ! valid_domain "$DOMAIN" || placeholder_domain "$DOMAIN"; then
        die "not a usable domain: $DOMAIN"
    fi
    ask COLLAB_DOMAIN "The document editor's domain" "collabora.$DOMAIN"
    if ! valid_domain "$COLLAB_DOMAIN" || placeholder_domain "$COLLAB_DOMAIN"; then
        die "not a usable domain: $COLLAB_DOMAIN"
    fi

    if [ "$NON_INTERACTIVE" = 0 ] && [ "$ASK_PROXY" = 1 ]; then
        log ""
        log "Reverse proxy:"
        log "  1) Caddy — installed and configured for you, with automatic HTTPS (recommended)"
        log "  2) None  — you run your own (e.g. nginx: docs/DEPLOYMENT.md → Reverse Proxy, Option B)"
        local reply
        read -r -p "Choice [1]: " reply
        case ${reply:-1} in 2) PROXY=none ;; *) PROXY=caddy ;; esac
    fi

    if [ "$NON_INTERACTIVE" = 0 ] && [ "$ASK_OCR" = 1 ]; then
        log ""
        log "OCR backend, which turns ranking screenshots into player rows:"
        log "  1) Cloud — Google Cloud Vision. Detects each screen by itself; needs a Google"
        log "             Cloud project and Vision API key (an admin sets it in the app later)."
        case $(pf_arch) in
            aarch64|arm64)
                log "  (Local OCR is not offered: its sidecar image is published for x86-64 only.)" ;;
            *)
                log "  2) Local — a PaddleOCR sidecar on this machine. No API cost, nothing leaves"
                log "             the network; you pick the screen per upload. ~2 GB image."
                local reply
                read -r -p "Choice [1]: " reply
                case ${reply:-1} in 2) OCR=local ;; *) OCR=cloud ;; esac ;;
        esac
    fi

    if [ "$NON_INTERACTIVE" = 0 ] && [ "$ARCHIVE" = 0 ] && [ "$OCR" = local ]; then
        log ""
        log "Local OCR request archival keeps uploaded screenshots and the parsed result, to"
        log "help improve OCR. It can grow past 10 GB over the retention window on this disk."
        confirm "Prepare local archiving (you switch it on in Admin → Security)?" && ARCHIVE=1
    fi
    if [ "$ARCHIVE" = 1 ] && [ "$OCR" != local ]; then
        log "Note: --archive prepares LOCAL-disk archival. For a Google Cloud Storage bucket, see"
        log "docs/IMAGE_RECOGNITION.md → GCS archival setup, then set it in Admin → Security."
        ARCHIVE=0
    fi
}

# rerun_detected — this directory already holds this install: it is registered here, or a
# previous run got as far as writing the marker before stopping.
rerun_detected() {
    [ "$(registry_name_for "$APP_DIR" || true)" != "" ] && return 0
    [ -f "$APP_DIR/.env" ] && ENV_FILE="$APP_DIR/.env" env_has HOST_FILES_VERSION
}

configure_firewall() {
    step "Firewall"
    # Allow first, enable last — enabling first refuses new SSH connections in the gap.
    # Any port sshd listens on is allowed, not just 22, so a moved SSH port is not locked out.
    local port ssh_ports
    ssh_ports=$(sudo ss -H -ltnp 2>/dev/null | awk '/"sshd"/ {n = split($4, a, ":"); print a[n]}' | sort -u)
    for port in 22 $ssh_ports; do
        sudo ufw allow "$port/tcp" comment 'SSH'
    done
    sudo ufw allow 80/tcp comment 'HTTP'
    sudo ufw allow 443/tcp comment 'HTTPS'
    sudo ufw --force enable
}

install_caddy() {
    if ! command -v caddy >/dev/null 2>&1; then
        step "Installing Caddy"
        sudo apt-get install -y debian-keyring debian-archive-keyring apt-transport-https
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
            | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
            | sudo tee /etc/apt/sources.list.d/caddy-stable.list > /dev/null
        sudo apt-get update
        sudo apt-get install -y caddy
    fi
    step "Configuring Caddy for $DOMAIN and $COLLAB_DOMAIN"
    install_caddyfile
    sudo systemctl enable caddy
    sudo systemctl restart caddy
}

# install_caddyfile — render the template, validate the RENDERED file, and only then put it
# in place. A different file already there is kept beside it, dated.
install_caddyfile() {
    local tmp
    tmp=$(mktemp)
    render_caddyfile "$APP_DIR/deploy/Caddyfile" "$DOMAIN" "$COLLAB_DOMAIN" > "$tmp"
    chmod 0644 "$tmp"
    # As the caddy user, so validating cannot leave a root-owned log file behind for the
    # service to fail on.
    if ! sudo -u caddy caddy validate --adapter caddyfile --config "$tmp" >/dev/null; then
        die "the rendered Caddyfile does not validate; it is at $tmp"
    fi
    if [ -e "$AM_CADDYFILE" ] && ! cmp -s "$tmp" "$AM_CADDYFILE"; then
        sudo cp -p "$AM_CADDYFILE" "$AM_CADDYFILE.backup_$(date +%Y%m%d_%H%M%S)"
    fi
    sudo install -D -m 0644 "$tmp" "$AM_CADDYFILE"
    rm -f "$tmp"
}

write_env() {
    step "Writing .env"
    local tag archive_dir=''
    tag=$(tr -d '[:space:]' < "$APP_DIR/HOST_FILES_VERSION")
    if [ ! -f "$APP_DIR/.env" ]; then
        (umask 077 && : > "$APP_DIR/.env")
    fi
    env_ensure SESSION_KEY "$(openssl rand -hex 32)"
    env_ensure CREDENTIAL_ENCRYPTION_KEY "$(openssl rand -hex 32)"
    env_ensure DATABASE_PATH /app/data/alliance.db
    env_ensure STORAGE_PATH /app/uploads
    env_ensure PRODUCTION true
    env_ensure HTTPS true
    env_ensure PORT 8080
    env_ensure APP_DOMAIN "$DOMAIN"
    env_ensure COLLABORA_DOMAIN "$COLLAB_DOMAIN"
    env_ensure TRUSTED_PROXY_COUNT 1
    # The pin is the release whose host files were just unpacked — not whatever the API
    # calls latest right now — so host files and image are one release by construction.
    env_ensure APP_VERSION "$tag"
    env_ensure OCR_BACKEND_MODE "$OCR"
    if [ "$OCR" = local ]; then
        env_ensure COMPOSE_FILE docker-compose.yml:docker-compose.local-ocr.yml
    fi
    if [ "$ARCHIVE" = 1 ]; then
        archive_dir=/app/data/ocr-archive
        env_ensure OCR_ARCHIVE_RETENTION_DAYS 7
    fi
    env_ensure OCR_ARCHIVE_DIR "$archive_dir"
    # Before `up`: compose reads env_file when it creates the container, and the Admin page
    # shows this beside the image version.
    env_ensure HOST_FILES_VERSION "$tag"
    if [ "$(env_get APP_VERSION)" != "$tag" ]; then
        warn "this .env pins APP_VERSION=$(env_get APP_VERSION), not the unpacked $tag — kept as it is"
    fi
}

start_stack() {
    step "Pulling and starting the containers"
    (cd "$APP_DIR" && sudo docker compose pull && sudo docker compose up -d)
}

# wait_for_app — until the app answers. A fresh install answers / with a redirect to /setup,
# so the wait follows redirects and takes the FINAL status; a bare poll of /login would see
# the redirect forever.
wait_for_app() {
    step "Waiting for the app to start"
    local code
    for _ in $(seq 1 "${AM_WAIT_TRIES:-45}"); do
        code=$(curl -sL -o /dev/null -w '%{http_code}' --max-time 5 http://127.0.0.1:8080/ 2>/dev/null || true)
        [ "$code" = 200 ] && return 0
        sleep "${AM_WAIT_INTERVAL:-2}"
    done
    warn "the app has not answered after $((${AM_WAIT_TRIES:-45} * ${AM_WAIT_INTERVAL:-2})) seconds."
    warn "Check its log: cd $APP_DIR && sudo docker compose logs alliance-manager"
    return 1
}

finish() {
    local key url="https://$DOMAIN/setup"
    log ""
    log "${C_GREEN}${C_BOLD}Alliance Manager $(env_get HOST_FILES_VERSION) is installed in $APP_DIR.${C_NC}"
    if sudo test -f "$APP_DIR/data/setup-key"; then
        key=$(sudo cat "$APP_DIR/data/setup-key")
        log ""
        log "Create the first administrator at:  $url"
        log "Setup key (valid 24 hours, works once):"
        log ""
        log "    $key"
        log ""
        log "It stays readable with: sudo cat $APP_DIR/data/setup-key"
    else
        log "This install already has accounts: log in at https://$DOMAIN/"
    fi
    if [ "$PROXY" = none ]; then
        log ""
        log "No proxy was configured. Put your own in front of 127.0.0.1:8080 and 127.0.0.1:9980"
        log "for $DOMAIN and $COLLAB_DOMAIN — docs/DEPLOYMENT.md → Reverse Proxy, Option B."
    fi
    log ""
    log "Updates:  $APP_DIR/scripts/manage.sh update      Status:  $APP_DIR/scripts/manage.sh status"
}

main() {
    parse_args "$@"
    APP_DIR=$(cd "$SCRIPT_DIR/.." && pwd)
    export APP_DIR
    AM_WRITER=install.sh
    PF_LOG="$APP_DIR/install.log"

    if [ ! -f "$APP_DIR/HOST_FILES_VERSION" ]; then
        die "no HOST_FILES_VERSION file beside scripts/ — install.sh runs from the unpacked host-files release asset:
    curl -fsSL -o host-files.tar.gz https://github.com/$AM_REPO/releases/latest/download/host-files.tar.gz
    tar -xzf host-files.tar.gz && ./scripts/install.sh"
    fi

    log "${C_BOLD}Alliance Manager installer — $(tr -d '[:space:]' < "$APP_DIR/HOST_FILES_VERSION"), into $APP_DIR${C_NC}"
    RERUN=0
    if rerun_detected; then
        RERUN=1
        log "This directory already holds this install; re-running adds whatever is missing."
    fi

    gather_choices

    local checks=(not-root sudo os existing-install legacy-service arch-ocr tools docker compose-v2 docker-running)
    PF_TOOLS="curl tar openssl sqlite3 ufw ss fail2ban sha256sum"
    if [ "$PROXY" = caddy ]; then
        checks+=(port-80 port-443 caddyfile)
        PF_TOOLS="$PF_TOOLS gpg"
    fi
    checks+=(port-8080 port-9980 dns disk memory)
    preflight_run "${checks[@]}"

    mkdir -p "$APP_DIR/data" "$APP_DIR/uploads"
    write_env
    configure_firewall
    if [ "$PROXY" = caddy ]; then
        install_caddy
    fi
    start_stack
    step "Registering the install"
    registry_write "$NAME" "$APP_DIR" || die "could not register the install (above)"
    ensure_backup_helper
    wait_for_app || true
    finish
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    main "$@"
fi
