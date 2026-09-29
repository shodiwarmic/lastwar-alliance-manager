# shellcheck shell=bash
# scripts/lib/preflight.sh — the prerequisite checks install.sh and manage.sh run first.
#
# Sourced, never executed; functions only. Needs common.sh and registry.sh sourced first.
#
# A check is a function check_<id> (dashes become underscores) that returns 0 or 1 and, on
# failure, sets PF_MSG to one line naming the CAUSE — "port 80 is held by nginx", not "port
# check failed". Each check is declared with pf_define: blocking or advisory, whether an
# operator may --ignore it, and an optional remediation function. preflight_run offers the
# remediation where one exists, re-runs every check after any remediation (a fix for one can
# change another's answer), and checks at most three times: a fix is only offered when there
# is a pass left to confirm it worked.
#
# Inputs the checks read, set by the caller: APP_DIR, PROXY (caddy|none), OCR (cloud|local),
# DOMAIN, COLLAB_DOMAIN, RERUN (1 when this directory already holds this install), PF_TOOLS
# (the binaries the `tools` check requires), PF_LOG (where ignored checks are recorded) and
# IGNORE (comma-separated ids from --ignore).

declare -gA PF_CLASS=() PF_IGNORABLE=() PF_REMEDY=()

# pf_define ID blocking|advisory ignorable(0|1) [REMEDY_FUNCTION]
pf_define() {
    PF_CLASS[$1]=$2
    PF_IGNORABLE[$1]=$3
    PF_REMEDY[$1]=${4:-}
}

# --- Seams the tests override ---------------------------------------------------------------
pf_arch()     { uname -m; }
# shellcheck source=/dev/null
pf_os_id()    { (. "${AM_OS_RELEASE:-/etc/os-release}" && printf '%s' "${ID:-}"); }
# shellcheck source=/dev/null
pf_codename() { (. "${AM_OS_RELEASE:-/etc/os-release}" && printf '%s' "${VERSION_CODENAME:-}"); }
pf_mem_kb()   { awk '/^MemTotal:/ {print $2}' "${AM_MEMINFO:-/proc/meminfo}"; }
pf_euid()     { printf '%s' "$EUID"; }

# public_ipv4 — this host's public IPv4 address as the internet sees it, or nothing.
public_ipv4() {
    local ip url
    for url in https://api.ipify.org https://ifconfig.me/ip; do
        ip=$(curl -4 -fsS --max-time 5 "$url" 2>/dev/null || true)
        if [[ $ip =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]; then
            printf '%s' "$ip"
            return 0
        fi
    done
}

# port_holder PORT — the name of the process listening on TCP PORT, or nothing if it is free.
# Needs sudo: unprivileged ss leaves the process column blank for other users' sockets,
# which is every daemon that matters here.
port_holder() {
    local line
    line=$(sudo ss -H -ltnp "sport = :$1" 2>/dev/null | head -n1)
    [ -n "$line" ] || return 0
    if [[ $line =~ users:\(\(\"([^\"]+)\" ]]; then
        printf '%s' "${BASH_REMATCH[1]}"
    else
        printf 'an unidentified process'
    fi
}

# docker_root_dir — where Docker keeps its images. Before Docker is installed, the nearest
# existing ancestor of the default, since df on a path that does not exist yet fails.
docker_root_dir() {
    local dir
    dir=$(sudo docker info -f '{{.DockerRootDir}}' 2>/dev/null || true)
    if [ -z "$dir" ]; then
        dir=/var/lib/docker
    fi
    while [ ! -e "$dir" ] && [ "$dir" != / ]; do
        dir=$(dirname "$dir")
    done
    printf '%s' "$dir"
}

# avail_kb PATH — free space on PATH's filesystem, in KiB.
avail_kb() { df -Pk "$1" | awk 'NR == 2 {print $4}'; }

# --- The checks -----------------------------------------------------------------------------

pf_define not-root blocking 0
check_not_root() {
    # Refuses ROOT. An ordinary user with sudo rights passes; the scripts call sudo exactly
    # where they need it, and files in the install directory stay the operator's.
    [ "$(pf_euid)" -ne 0 ] && return 0
    PF_MSG="running as root — run this as an ordinary user with sudo rights"
    return 1
}

pf_define sudo blocking 0
check_sudo() {
    command -v sudo >/dev/null 2>&1 && return 0
    PF_MSG="sudo is not installed"
    return 1
}

pf_define os blocking 0
check_os() {
    local id
    id=$(pf_os_id)
    case $id in debian|ubuntu) return 0 ;; esac
    PF_MSG="unsupported OS '${id:-unknown}': this script supports Debian and Ubuntu. On anything else, follow the manual Docker steps in docs/DEPLOYMENT.md"
    return 1
}

pf_define existing-install blocking 0
check_existing_install() {
    # An entry for THIS directory is a re-run, which every later step tolerates. An entry
    # for another directory means this host already runs an install — and a second one
    # would collide with it on container names and ports.
    local name dir
    while IFS= read -r name; do
        dir=$(registry_get "$name")
        if [ -n "$dir" ] && [ "$dir" != "$APP_DIR" ]; then
            PF_MSG="this host already has an install ('$name') at $dir — update it with $dir/scripts/manage.sh update. If it no longer exists, delete $(registry_dir)/$name.conf"
            return 1
        fi
    done < <(registry_list)
    return 0
}

pf_define legacy-service blocking 0
check_legacy_service() {
    systemctl is-active --quiet lastwar.service 2>/dev/null || return 0
    PF_MSG="the pre-Docker lastwar.service is running on this host. Its data (alliance.db and files/) is in /var/lib/lastwar, which this installer does not import. To keep that data, convert this server to Docker with the v1.1.0 scripts and then run scripts/manage.sh migrate (see the v2.0.0 release notes). To start fresh instead: sudo systemctl disable --now lastwar.service"
    return 1
}

pf_define arch-ocr blocking 0
check_arch_ocr() {
    [ "${OCR:-cloud}" = local ] || return 0
    case $(pf_arch) in
        aarch64|arm64)
            PF_MSG="the local OCR sidecar image is published for x86-64 only; use --ocr cloud on this ARM host"
            return 1 ;;
    esac
    return 0
}

# The binary each tool comes from. Only the names that differ are listed; everything else
# is installed under its own name.
declare -gA PF_PACKAGE=([ss]=iproute2 [gpg]=gnupg [sha256sum]=coreutils [crontab]=cron)

# Where a tool may live that an ordinary user's PATH does not reach: Debian's default user PATH
# has no sbin, so `command -v ufw` fails for the (deliberately non-root) operator even once ufw
# is installed. The scripts run such tools through sudo, whose secure_path does include them.
PF_SBIN_DIRS=${AM_SBIN_DIRS:-/usr/local/sbin /usr/sbin /sbin}

pf_tool_present() {
    local d
    case $1 in
        fail2ban) dpkg -s fail2ban >/dev/null 2>&1; return ;;
    esac
    command -v "$1" >/dev/null 2>&1 && return 0
    for d in $PF_SBIN_DIRS; do
        [ -x "$d/$1" ] && return 0
    done
    return 1
}

pf_missing_tools() {
    local t
    for t in ${PF_TOOLS:-}; do
        pf_tool_present "$t" || printf '%s\n' "$t"
    done
}

pf_define tools blocking 0 remedy_tools
check_tools() {
    local missing
    missing=$(pf_missing_tools | tr '\n' ' ')
    [ -z "$missing" ] && return 0
    PF_MSG="missing: ${missing% }"
    return 1
}
remedy_tools() {
    local t pkgs=()
    while IFS= read -r t; do
        pkgs+=("${PF_PACKAGE[$t]:-$t}")
    done < <(pf_missing_tools)
    [ ${#pkgs[@]} -gt 0 ] || return 0
    apt_update
    apt_install "${pkgs[@]}"
}

pf_define docker blocking 0 remedy_docker
check_docker() {
    command -v docker >/dev/null 2>&1 && return 0
    PF_MSG="Docker is not installed"
    return 1
}
remedy_docker() {
    # Docker's own apt repository, as its install guide gives it.
    local os codename
    os=$(pf_os_id)
    codename=$(pf_codename)
    apt_update
    apt_install ca-certificates curl
    sudo install -m 0755 -d /etc/apt/keyrings
    sudo curl -fsSL "https://download.docker.com/linux/$os/gpg" -o /etc/apt/keyrings/docker.asc
    sudo chmod a+r /etc/apt/keyrings/docker.asc
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$os $codename stable" \
        | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
    apt_update
    apt_install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
}

pf_define compose-v2 blocking 0 remedy_compose_v2
check_compose_v2() {
    docker compose version >/dev/null 2>&1 && return 0
    if ! command -v docker >/dev/null 2>&1; then
        PF_MSG="Docker is not installed, so neither is Compose"
    elif command -v docker-compose >/dev/null 2>&1; then
        PF_MSG="only the old docker-compose (v1) is installed; Compose v2 ('docker compose') is required"
    else
        PF_MSG="the Docker Compose plugin ('docker compose') is not installed"
    fi
    return 1
}
remedy_compose_v2() {
    if ! command -v docker >/dev/null 2>&1; then
        remedy_docker
        return
    fi
    # docker-compose-plugin is the name in Docker's repository; Ubuntu's own is docker-compose-v2.
    apt_install docker-compose-plugin || apt_install docker-compose-v2
}

pf_define docker-running blocking 0 remedy_docker_running
check_docker_running() {
    systemctl is-active --quiet docker 2>/dev/null && return 0
    PF_MSG="the Docker service is not running"
    return 1
}
remedy_docker_running() {
    sudo systemctl enable --now docker
}

# pf_check_port PORT EXPECTED — free, or (on a re-run of this same install) held by the
# process this install itself put there: caddy on 80/443, docker-proxy on 8080/9980.
pf_check_port() {
    local port=$1 expected=$2 holder
    holder=$(port_holder "$port")
    [ -z "$holder" ] && return 0
    if [ "${RERUN:-0}" = 1 ] && [ "$holder" = "$expected" ]; then
        return 0
    fi
    PF_MSG="port $port is in use by $holder"
    if [ "$holder" = nginx ] && { [ "$port" = 80 ] || [ "$port" = 443 ]; }; then
        PF_MSG="$PF_MSG — to keep nginx as the proxy, re-run with --proxy none and configure it as in docs/DEPLOYMENT.md (Reverse Proxy, Option B)"
    fi
    return 1
}
pf_define port-80 blocking 1
check_port_80()   { pf_check_port 80 caddy; }
pf_define port-443 blocking 1
check_port_443()  { pf_check_port 443 caddy; }
pf_define port-8080 blocking 1
check_port_8080() { pf_check_port 8080 docker-proxy; }
pf_define port-9980 blocking 1
check_port_9980() { pf_check_port 9980 docker-proxy; }

pf_define caddyfile blocking 1
check_caddyfile() {
    # Ours (it carries the revision marker), or none yet. Ignoring this means the existing
    # file is backed up, dated, and replaced.
    [ ! -e "$AM_CADDYFILE" ] && return 0
    [ "$(caddyfile_rev "$AM_CADDYFILE")" -gt 0 ] && return 0
    PF_MSG="$AM_CADDYFILE exists and was not written by this installer — it would be backed up and replaced"
    return 1
}

pf_define dns blocking 1
check_dns() {
    # Blocking behind Caddy, which cannot obtain certificates for a name that does not point
    # here. Advisory with --proxy none (TLS may end somewhere else entirely) and whenever
    # this host's public address cannot be found, since then nothing was actually checked.
    local pub d addrs bad=()
    [ "${PROXY:-caddy}" = caddy ] || PF_RESULT_CLASS=advisory
    pub=$(public_ipv4)
    if [ -z "$pub" ]; then
        PF_RESULT_CLASS=advisory
        PF_MSG="could not determine this host's public IPv4 address, so DNS was not checked"
        return 1
    fi
    for d in "$DOMAIN" "$COLLAB_DOMAIN"; do
        addrs=$(getent ahosts "${d%%:*}" 2>/dev/null | awk '{print $1}' | sort -u | tr '\n' ' ')
        if ! grep -qw -- "$pub" <<<"$addrs"; then
            bad+=("$d → ${addrs:-nothing}")
        fi
    done
    [ ${#bad[@]} -eq 0 ] && return 0
    PF_MSG="not pointing at this host ($pub): $(IFS=';'; echo "${bad[*]}"). No certificate can be issued for them until they do. If a CDN or another proxy fronts this host, that is expected: --ignore dns"
    return 1
}

pf_define disk blocking 1
check_disk() {
    # Two filesystems, named separately: the install directory's (the database, uploads, any
    # local OCR archive) and Docker's data root, where the images actually land — Collabora
    # ~1.5 GB, the app, and local OCR ~2 GB plus 250 MB of models.
    local data_kb img_kb img_dir notes=()
    data_kb=$(avail_kb "$APP_DIR")
    img_dir=$(docker_root_dir)
    img_kb=$(avail_kb "$img_dir")
    if [ "$img_kb" -lt $((2 * 1024 * 1024)) ]; then
        PF_MSG="only $((img_kb / 1024)) MB free for Docker images on $img_dir (at least 2 GB needed)"
        return 1
    fi
    PF_RESULT_CLASS=advisory
    [ "$img_kb" -lt $((5 * 1024 * 1024)) ] && notes+=("$((img_kb / 1024)) MB free for Docker images on $img_dir")
    [ "$data_kb" -lt $((5 * 1024 * 1024)) ] && notes+=("$((data_kb / 1024)) MB free for data on $APP_DIR")
    [ ${#notes[@]} -eq 0 ] && return 0
    PF_MSG="$(IFS=';'; echo "${notes[*]}") — 5 GB or more is recommended"
    return 1
}

pf_define memory advisory 1
check_memory() {
    local kb
    kb=$(pf_mem_kb)
    # A "2 GB" machine reports a little under 2 GiB once the kernel has taken its share.
    [ "${kb:-0}" -ge 1900000 ] && return 0
    PF_MSG="$((${kb:-0} / 1024)) MB of RAM — 2 GB or more is recommended (Collabora alone uses several hundred MB)"
    return 1
}

# --- The loop -------------------------------------------------------------------------------

pf_ignored() {
    [[ ",${IGNORE:-}," == *",$1,"* ]]
}

pf_record_ignore() {
    if [ -n "${PF_LOG:-}" ]; then
        printf '%s IGNORED %s: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$2" >> "$PF_LOG"
    fi
}

# preflight_run ID... — run the checks, remediating and re-running as needed. Returns 0 when
# nothing blocking is left; exits 1 otherwise, naming the ids.
preflight_run() {
    local pass id fn class remedied choice can_fix interactive=1
    local -a blocking unresolved
    local -A msg
    # No terminal to answer on (a pipe, cron, automation) is non-interactive too: a prompt
    # there would read end-of-file and stop the run for no reason it could show.
    if [ "${NON_INTERACTIVE:-0}" = 1 ] || [ ! -t 0 ]; then
        interactive=0
    fi
    for pass in 1 2 3; do
        blocking=()
        unresolved=()
        remedied=0
        log "Checking prerequisites$([ "$pass" -gt 1 ] && printf ' (pass %d)' "$pass")..."
        for id in "$@"; do
            fn="check_${id//-/_}"
            PF_MSG=''
            PF_RESULT_CLASS=${PF_CLASS[$id]}
            if "$fn"; then
                printf '  %s✔%s %s\n' "$C_GREEN" "$C_NC" "$id"
                continue
            fi
            class=$PF_RESULT_CLASS
            msg[$id]=$PF_MSG
            if [ "$class" = advisory ]; then
                printf '  %s!%s %s: %s\n' "$C_YELLOW" "$C_NC" "$id" "$PF_MSG"
            else
                printf '  %s✘%s %s: %s\n' "$C_RED" "$C_NC" "$id" "$PF_MSG"
                blocking+=("$id")
            fi
        done
        [ ${#blocking[@]} -eq 0 ] && return 0

        for id in "${blocking[@]}"; do
            can_fix=''
            if [ -n "${PF_REMEDY[$id]}" ] && [ "$pass" -lt 3 ]; then
                can_fix=1
            fi
            if pf_ignored "$id" && [ "${PF_IGNORABLE[$id]}" = 1 ]; then
                log "  ignoring $id (--ignore)"
                pf_record_ignore "$id" "${msg[$id]}"
                continue
            fi
            if [ "$interactive" = 0 ]; then
                if [ -n "$can_fix" ]; then
                    step "Fixing $id"
                    "${PF_REMEDY[$id]}"
                    remedied=1
                else
                    unresolved+=("$id")
                fi
                continue
            fi
            # Interactive: offer what applies to this check, and nothing that does not.
            local prompt="  $id — "
            [ -n "$can_fix" ] && prompt+="[f]ix it, "
            [ "${PF_IGNORABLE[$id]}" = 1 ] && prompt+="[i]gnore it, "
            prompt+="or e[x]it? "
            while true; do
                read -r -p "$prompt" choice
                case $choice in
                    f|F) if [ -n "$can_fix" ]; then step "Fixing $id"; "${PF_REMEDY[$id]}"; remedied=1; break; fi ;;
                    i|I) if [ "${PF_IGNORABLE[$id]}" = 1 ]; then
                             pf_record_ignore "$id" "${msg[$id]}"
                             IGNORE="${IGNORE:+$IGNORE,}$id"
                             break
                         fi ;;
                    x|X) exit 1 ;;
                esac
            done
        done

        if [ $remedied = 0 ]; then
            if [ ${#unresolved[@]} -gt 0 ] && [ "$pass" = 3 ]; then
                die "prerequisites still not met after three checks: ${unresolved[*]}"
            fi
            if [ ${#unresolved[@]} -gt 0 ]; then
                local hint=''
                for id in "${unresolved[@]}"; do
                    if [ "${PF_IGNORABLE[$id]}" = 1 ]; then
                        hint=" (checks marked ignorable can be passed with --ignore <id>,<id>, recorded in ${PF_LOG:-the log})"
                    fi
                done
                die "prerequisites not met: ${unresolved[*]}$hint"
            fi
            return 0
        fi
    done
}
