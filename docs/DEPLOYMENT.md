# Production Deployment Guide

This guide covers the deployment of the Last War Alliance Manager. Because the application and its dependencies (Go, Collabora CODE) are fully containerized using Docker, the host server setup is incredibly lightweight.

## 1. DNS Configuration (Pre-Requisite)

Before beginning the deployment, you **must** configure your domain's DNS. The document management system requires a dedicated subdomain to route traffic correctly.

Create two A-Records pointing to your server's IP address:
1. **Main App:** `app.yourdomain.com` → `your.server.ip.address`
2. **Collabora:** `collabora.yourdomain.com` → `your.server.ip.address`

Wait for DNS propagation (5-60 minutes) before proceeding to the reverse proxy steps, or SSL certificate generation will fail.

---

## 2. Automated Setup (Recommended)

The easiest way to deploy the application on a fresh Debian or Ubuntu server is the installation
script. What a host needs — the compose files, the scripts and the proxy template — ships as a
small versioned release asset, `host-files.tar.gz`; there is nothing to clone. Pick any directory
for the install and run:

```bash
mkdir -p ~/alliance-manager && cd ~/alliance-manager      # any directory you like
curl -fsSL -o host-files.tar.gz \
  https://github.com/shodiwarmic/lastwar-alliance-manager/releases/latest/download/host-files.tar.gz
tar -xzf host-files.tar.gz && rm host-files.tar.gz
./scripts/install.sh
```

To install a specific release instead of the newest, use
`https://github.com/shodiwarmic/lastwar-alliance-manager/releases/download/v2.0.0/host-files.tar.gz`
(any tag from v2.0.0 on). Each release also publishes `host-files.tar.gz.sha256`; to check a
download, fetch it beside the tarball and run `sha256sum -c host-files.tar.gz.sha256`.

The script checks the host first — OS, Docker and Compose, free ports, DNS, disk and memory —
and offers to fix what it can (installing Docker or missing packages). It then generates `.env`
with fresh secrets, configures the firewall, installs Caddy with automatic HTTPS, starts the
stack pinned to the release you downloaded, registers the install in
`/etc/alliance-manager/installs.d/`, sets up nightly backups, and prints the one-time **setup key**
for creating the first administrator.

Every question has a flag, so it can also run unattended:

```bash
./scripts/install.sh --non-interactive --domain app.yourdomain.com
```

`--help` lists the rest: `--collabora-domain`, `--proxy none` (bring your own proxy — see
Option B below), `--ocr local`, `--archive`, and `--ignore <check>` for a prerequisite check you
have a reason to override (for example `--ignore dns` when a CDN fronts the host). Ignored checks
are recorded in `install.log`. Re-running the script in the same directory is safe.

---

## 3. Manual Docker Deployment

If you prefer to set up the infrastructure yourself or are integrating this into an existing Docker environment, follow these steps.

### Step A: Install Docker
Ensure Docker and Docker Compose are installed on your system. 
[Official Docker Installation Guide](https://docs.docker.com/engine/install/)

### Step B: Prepare the Environment
```bash
# Download and unpack the host files into the directory you want the install in
mkdir -p ~/alliance-manager && cd ~/alliance-manager
curl -fsSL -o host-files.tar.gz \
  https://github.com/shodiwarmic/lastwar-alliance-manager/releases/latest/download/host-files.tar.gz
tar -xzf host-files.tar.gz && rm host-files.tar.gz

# Create persistent storage directories
mkdir -p ./data ./uploads

# Configure environment variables
cp .env.example .env
nano .env

# Pin the image to the release you downloaded, and mark the host files as that release
# (./scripts/manage.sh update reads the second line)
echo "APP_VERSION=$(cat HOST_FILES_VERSION)" >> .env
echo "HOST_FILES_VERSION=$(cat HOST_FILES_VERSION)" >> .env
```

Ensure your `.env` contains secure values. You **must** generate random 32-byte hex strings for both the session key and the credential encryption key.
*(You can generate these by running `openssl rand -hex 32` in your terminal).*
With `PRODUCTION=true` the app refuses to start when `SESSION_KEY` is unset or shorter than 32 characters.

```env
SESSION_KEY=your_generated_64_character_hex_string_here
CREDENTIAL_ENCRYPTION_KEY=your_second_generated_64_character_hex_string_here
DATABASE_PATH=/app/data/alliance.db
STORAGE_PATH=/app/uploads
PRODUCTION=true
HTTPS=true
APP_DOMAIN=app.yourdomain.com
COLLABORA_DOMAIN=collabora.yourdomain.com
TRUSTED_ORIGINS=localhost:8080, 127.0.0.1:8080
```

#### Optional variables

These have working defaults and are absent from `.env.example`; set them only if you need them.

| Variable | Default | Purpose |
|---|---|---|
| `APP_VERSION` | `latest` | Which published image this install runs (`v1.2.3`, the moving `v1.2`, `latest`, or `edge`). `scripts/install.sh` and `scripts/manage.sh update` write the release tag here for you, together with the host files of the same release — pin or roll back with `./scripts/manage.sh update --version vX.Y.Z` rather than by hand. `latest` means the latest *release*; `edge` is built from `main`. See [RELEASING.md](RELEASING.md). |
| `PORT` | `8080` | Port the Go application listens on inside its container. Change it only if you also change the published port in `docker-compose.yml`. |
| `WOPI_INTERNAL_HOST` | `alliance-manager:8080` | Where Collabora reaches the app for its WOPI callbacks (file info, load, save): the app's service name and internal port on the compose network. Read by the app *and* by `docker-compose.yml` (Collabora's `aliasgroup2`), so one line in `.env` moves both. Change it only for another service name, Collabora on another host, or a changed internal port. |
| `UPDATE_CHECK` | `on` | The daily release check: once a day the app asks GitHub for the newest release (one unauthenticated `GET` to `api.github.com/repos/shodiwarmic/lastwar-alliance-manager/releases/latest`, the request `manage.sh update` already makes, with a `User-Agent` carrying no version) and tells administrators on **Admin → Security & API**. Set `off` to never make the request; a demo install never makes it. |
| `COLLABORA_PORT` | *(unset)* | Explicit port for the Collabora document server, appended to `COLLABORA_DOMAIN` when building WOPI URLs. Needed only when Collabora is reached on a non-standard port rather than through the reverse proxy. |
| `TRUSTED_PROXY_COUNT` | `1` when `PRODUCTION=true`, else `0` | How many reverse proxies sit in front of the app. The client address every login rate limit keys on is read that many entries from the right of `X-Forwarded-For`; entries further left were sent by the client and are ignored. `0` ignores the header and uses the TCP peer. Set `2` if a CDN or load balancer sits in front of your Caddy or nginx. On an unproxied install running `PRODUCTION=true`, set `0` — otherwise a client could choose its own rate-limit bucket. |
| `BIND_ADDR` | `127.0.0.1` | Read by `docker-compose.yml`, not by the app: the address ports 8080 and 9980 are published on. The default keeps them reachable only through the reverse proxy on the same host (Docker's published ports bypass `ufw`). Set `0.0.0.0` for a LAN or development install with no proxy. The development stack's `.env` entries, this one included, are in `deploy/.env.dev.example`. |
| `OCR_BACKEND_MODE` | `cloud` | Set to `local` to use the bundled PaddleOCR sidecar instead of Google Cloud Vision. Also requires `COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml`. `scripts/install.sh` sets both for you if you opt in. See [IMAGE_RECOGNITION.md](IMAGE_RECOGNITION.md). |

Two further optional variables, `OCR_ARCHIVE_DIR` and `OCR_ARCHIVE_RETENTION_DAYS`, configure
local-disk OCR archival and are documented under [OCR Request Archival](#ocr-request-archival-optional) below.

**Choosing the OCR backend.** The default cloud backend (Google Cloud Vision) needs nothing more:
`docker-compose.yml` alone runs it. For the **local** PaddleOCR sidecar, set both of these in
`.env` *before* the first `up`, because the sidecar service is defined only in the second
compose file:

```env
OCR_BACKEND_MODE=local
COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml
```

### Step C: Pull and Start the Stack
```bash
sudo docker compose pull
sudo docker compose up -d
```
This downloads the pre-built images (there is nothing to build), creates a private internal
bridge network, and starts the Go application and the Collabora document server, published on
`127.0.0.1:8080` and `127.0.0.1:9980` — reachable by a reverse proxy on the same host, not from
outside it. For a LAN install with no proxy, set `BIND_ADDR=0.0.0.0` in `.env` first.

#### Architecture support (x86-64 and ARM)

The application image is published for both **linux/amd64** and **linux/arm64**, so it runs on
an ARM server — AWS Graviton, Ampere, or a 64-bit Raspberry Pi — as well as on x86-64.
`docker compose pull` selects the right one for your host automatically; there is nothing to
configure. The pinned Collabora image is multi-arch too.

One exception: the **local OCR backend is x86-64 only**. Its PaddleOCR sidecar image is not
published for ARM, so on an ARM host use the default cloud backend (Google Cloud Vision) — see
[IMAGE_RECOGNITION.md](IMAGE_RECOGNITION.md). Everything else works the same on either
architecture.

### Step D: Create the first administrator
The app ships with no account. It writes a one-time setup key to `data/setup-key` on first start;
read it and open `https://app.yourdomain.com/setup` (every page redirects there until the first
administrator exists):

```bash
sudo cat data/setup-key
```

The key is valid for 24 hours and works once. To issue a new one, delete the file and run
`sudo docker compose restart alliance-manager`.

---

## 4. Reverse Proxy Configuration

You must put the Docker containers behind a reverse proxy to handle SSL termination. 

### Option A: Caddy (Recommended)
Caddy automatically handles Let's Encrypt certificates and WebSockets natively, and it is what
`scripts/install.sh` sets up. The configuration is one template in the host files,
`deploy/Caddyfile`, with two placeholders for your domains. To render it by hand from the install
directory:

```bash
sed -e 's|__APP_DOMAIN__|app.yourdomain.com|g' \
    -e 's|__COLLABORA_DOMAIN__|collabora.yourdomain.com|g' \
    deploy/Caddyfile | sudo tee /etc/caddy/Caddyfile >/dev/null
sudo systemctl reload caddy
```

The first line of the rendered file carries the template's revision
(`# alliance-manager caddyfile rev N`). Keep it: `./scripts/manage.sh update` re-renders
`/etc/caddy/Caddyfile` — backing up the old one, dated, beside it — whenever a release ships a
higher revision, and leaves the file alone otherwise. A hand edit is therefore overwritten by the
next revision; a `www.` redirect, if your DNS has that record, is one such addition (the template
shows the block).

The app compresses its own text responses (HTML, CSS, JS, JSON, SVG, CSV) with gzip, so an install
reached without a proxy is not served full-size assets. Caddy's `encode gzip` passes an
already-encoded response through untouched, and nginx's `gzip` does the same.

The project's public demo runs on Cloud Run without a proxy, in demo mode — a different
deployment from an install; see `docs/DEMO.md`.

### Option B: Nginx
The installer does not configure nginx: install with `./scripts/install.sh --proxy none`, which
sets up everything except the proxy, then configure nginx yourself. Ensure you have `certbot` and
`python3-certbot-nginx` installed for SSL. Create `/etc/nginx/sites-available/lastwar`:

```nginx
# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name app.yourdomain.com collabora.yourdomain.com;
    return 301 https://$host$request_uri;
}

# Main Go Application
server {
    listen 443 ssl http2;
    server_name app.yourdomain.com;
    
    # SSL config injected by certbot...
    
    add_header X-Frame-Options "DENY" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com; img-src 'self' data: https://lastwar-cdn.akamaized.net https://lastwar-cdn.lastwarapp.net; font-src 'self' https://fonts.gstatic.com; connect-src 'self' https://collabora.yourdomain.com; frame-src https://collabora.yourdomain.com; frame-ancestors 'none';" always;
    
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

# Collabora Document Server
server {
    listen 443 ssl http2;
    server_name collabora.yourdomain.com;
    
    # SSL config injected by certbot...

    # Strict CSP to allow only your main app to iframe the document editor
    add_header Content-Security-Policy "frame-ancestors https://app.yourdomain.com" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    
    location / {
        proxy_pass http://127.0.0.1:9980;
        proxy_set_header Host $host;
    }

    # WebSockets for Collabora Document Editing
    location ~ ^/cool/(.*)/ws$ {
        proxy_pass http://127.0.0.1:9980;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 36000s;
    }
    
    location ^~ /cool/adminws {
        proxy_pass http://127.0.0.1:9980;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 36000s;
    }
}
```
*Run `sudo certbot --nginx -d app.yourdomain.com -d collabora.yourdomain.com` to apply SSL.*

Keep the `X-Forwarded-For $proxy_add_x_forwarded_for` line: nginx appends the real client
address to the end of the header, and the app reads it from there — one entry from the right,
which is the default for `PRODUCTION=true` (`TRUSTED_PROXY_COUNT=1`). The app does not read
`X-Real-IP`; a client can send that header as easily as a forged `X-Forwarded-For`.

---

## 5. Additional Security Hardening

### Configure Firewall (UFW)
Ensure only necessary ports are exposed to the internet. `scripts/install.sh` does this for you.
Note that `ufw` does not govern ports Docker publishes — Docker's own iptables rules sit in front
of it — which is why `docker-compose.yml` publishes the app and Collabora on `127.0.0.1` only.
```bash
sudo ufw allow 22/tcp  # SSH
sudo ufw allow 80/tcp  # HTTP
sudo ufw allow 443/tcp # HTTPS
sudo ufw enable
```

### Database Backups
Backups land in `/var/backups/lastwar/`, in two pools with different retention rules:

| Pool | Written by | What | Kept |
|---|---|---|---|
| `nightly_<install>_<timestamp>.db` | `/usr/local/bin/backup-lastwar.sh`, from root's crontab at 02:00 | The database of every registered install | 7 days |
| `db_<timestamp>.db` and `app_<timestamp>.tar.gz` | `./scripts/manage.sh`, before every update (or on demand: `./scripts/manage.sh backup`) | The database, and the install directory without `data/` (so including `uploads/` — the alliance's files) | The newest 10 of each |

They are kept apart on purpose: with a single newest-ten rule, ten nightly backups would push out
every pre-update restore point in ten days.

The nightly helper is root-owned and self-contained — it runs nothing from the install directory,
reads the install list from `/etc/alliance-manager/installs.d/`, and runs `sqlite3 .backup`
itself. To take one by hand: `sudo /usr/local/bin/backup-lastwar.sh`.

### OCR Request Archival (optional)
Archival is off by default and configured by an admin in **Admin → Security → OCR
Request Archival**. The GCS bucket name + destination are set in that UI; the GCP
bucket/IAM setup (bucket creation, 14-day lifecycle rule, `Storage Object Creator`
grant) is documented in **IMAGE_RECOGNITION.md → GCS archival setup**.

For **local-disk** archival:

- **Where archives land:** set `OCR_ARCHIVE_DIR` in `.env`. The default
  `/app/data/ocr-archive` already persists to the host via the existing
  `./data:/app/data` mount (passed through `env_file: .env`) — **no new volume
  needed**. ⚠️ A custom path *outside* a mounted volume writes to the container's
  ephemeral layer and is **lost on the next `docker compose up`/recreate**; to use
  another disk, bind-mount it at the host's `./data/ocr-archive` rather than
  repointing `OCR_ARCHIVE_DIR` to an unmounted container path.
- **Disk usage:** an active alliance can accumulate **10 GB+** over the retention
  window. Ensure the host disk has headroom — if `./data` fills, the SQLite
  database can halt/corrupt.
- **Retention (default, in-app):** an in-app janitor prunes `OCR_ARCHIVE_DIR` to
  `OCR_ARCHIVE_RETENTION_DAYS` (default `7`) automatically — nothing to set up.
- **Retention (OS-native alternative):** set `OCR_ARCHIVE_RETENTION_DAYS` to a
  negative value to disable the in-app janitor, then manage cleanup yourself, e.g.
  a `systemd-tmpfiles.d` rule (`d <install directory>/data/ocr-archive 0750 <user> <group> 7d`)
  or a cron entry:
  ```bash
  find <install directory>/data/ocr-archive -mindepth 1 -maxdepth 1 -type d -mtime +7 -exec rm -rf {} +
  ```

---

## 6. Update Procedure

An install is updated with `scripts/manage.sh`, from the install directory:

```bash
./scripts/manage.sh update
```

It moves the **host files and the image together** to the newest release: it downloads that
release's `host-files.tar.gz` and checks it against its published checksum, takes a backup (see
[Database Backups](#database-backups)), pins `APP_VERSION`, pulls the new images, and only then
replaces the compose files and scripts — removing any a release has stopped shipping — checks
the result is still a valid stack, re-renders the Caddyfile if the release ships a newer
revision, and restarts the containers. If any step before the restart fails, the install is put
back exactly as it was and the running containers are left alone. Files you added to the install
directory yourself are never touched. If the script cannot reach GitHub it changes nothing,
rather than silently moving your install onto something else.

The update also moves the two third-party images the compose files pin — the Collabora document
server and, on a local-OCR install, the OCR sidecar — to the versions the new release was tested
with, and pulls them before anything on the host changes. Neither follows its publisher's
`latest`: a release names the exact images it was tested with. `COLLABORA_IMAGE` and
`OCR_LOCAL_IMAGE` in `.env` override those pins (see `.env.example`), and a value set there
outlives every later release, so leave them unset unless you mean to own that choice.

The update runs in two stages: the copy of `manage.sh` already on disk fetches and unpacks the
new release into `.staging/`, then hands over to the **new** release's own copy, which does the
work. So a release's changes to the updater take effect in the run that delivers them.

Only one update of an install runs at a time: the update holds a lock on `.update.lock` in the
install directory from start to finish, and a second one started meanwhile — from another
terminal, or from the Admin page — stops at once with *another update of this install is
running*, changing nothing. The nightly backup skips an install while its update runs. Leave the
file in place; it is empty.

`./scripts/update.sh`, the updater before v2.0.0, still exists and simply runs
`./scripts/manage.sh update` for you.

### Pinning and rolling back

`--version` moves the install to a specific release — newer or older — host files and image
together:

```bash
./scripts/manage.sh update --version v2.0.0
```

Releases before v2.0.0 have no host-files asset, so they cannot be targeted this way. To run such
a release's image, set `APP_VERSION` in `.env` by hand and `sudo docker compose pull && sudo docker
compose up -d`; the host files stay as they are.

A rollback does **not** roll back `/etc/caddy/Caddyfile`: a newer revision is left in place in
front of the older release. Revisions only ever add hosts and headers, and `status` shows the
deployed revision beside the release's own, so the difference is visible.

Published tags are `vX.Y.Z` (exact release), `vX.Y` (moves with its patches), `latest` (the
newest release) and `edge` (built from `main` — development, not a release). An install pinned
to `edge` is not updated by `manage.sh`: there are no host files built for it. What each release
level promises about an update is set out in [RELEASING.md](RELEASING.md).

> **There is nothing to build.** The production compose file has no `build:` key — the image is
> pre-built and published for you. Building from source is a development workflow and needs the
> dev override (`deploy/docker-compose.override.yml.example`), which ignores `APP_VERSION` by
> design.

### Checking what is running

```bash
./scripts/manage.sh status
```

prints the install directory and its registry entry, the release of the image and of the host
files, the deployed Caddyfile revision beside the one this release ships, the image the container
is actually running and its health, its startup line, and `docker compose ps`. The same versions
are on **Admin → Security & API → About this install**. Quote both when reporting a problem.

From v3.0.0 the image carries a Docker `HEALTHCHECK`: every 30 seconds the container probes its
own `GET /api/health`, which answers `200 {"status":"ok"}` when the app is up and its database
answers a query, and `503` otherwise. `docker ps` shows the state (`healthy`, `unhealthy`, or
`starting` for the first 90 seconds, while migrations run). The route needs no login, says nothing
else about the install, and is safe to point an external uptime monitor at.

`./scripts/manage.sh backup` takes the pre-update backup on its own, at any time.

### Updating from the Admin page

From v3.0.0 an administrator can install a release from **Admin → Security & API**. The app
cannot change the server itself — it has no access to Docker or to the host — so a small
**host update agent** does it on the app's behalf. `./scripts/manage.sh update` (and
`install.sh`) installs it; v3.0.0 itself is the last release that has to be installed from a
terminal.

What is on the server:

| Path | What it is |
|---|---|
| `/usr/local/bin/alliance-manager-update.sh` | The agent. Root-owned, rewritten by every update. |
| `/etc/systemd/system/alliance-manager-update.{service,timer}` | The timer runs the agent every 30 seconds. |
| `/var/lib/alliance-manager/<install>/` | The agent's own state, root-only: the journal of the current request, a copy of it, the receipt the updater writes, the rollback bundle, and each request's full log (`update-<id>.log`). |
| `data/update/` in the install directory | Where the app and the agent talk: the request, the agent's heartbeat and progress, each request's result and a tail of its log. |
| `/var/backups/lastwar/pinned/<id>.db` | The pre-update database backup of a request, held outside the newest-ten rotation until the request is finished. |

Each tick the agent writes a heartbeat (the Admin page shows the agent as active, with how long
ago it was last seen) and looks for a request. A request runs `./scripts/manage.sh update
--version <release>` — the same update a terminal runs, with the same backup and the same
rollback of the files — as root, with the app holding off every change until the update is
checked. Then the agent waits up to five minutes for the new release to report healthy and checks
it is running the image it asked for (and, when the release moved Collabora or the OCR sidecar,
that those came up). If anything fails, it puts the previous release back **without the network**,
from a copy of the host files it took first and the previous images it held on to; the database
is restored from the pre-update backup only when a migration had run and the app accepted no
change meanwhile.

The agent runs only the install's own `scripts/manage.sh`, and only after checking that it and
everything it loads — and every directory above the install — belong to you or root and are
not writable by anyone else; it refuses an install where they are, and says why on the Admin
page and in its log. It never runs anything from `data/` or `uploads/`, which the app can write.

Its log is in the system journal:

```bash
journalctl -u alliance-manager-update        # what the agent did, request by request
sudo less /var/lib/alliance-manager/default/update-<id>.log   # one request in full
./scripts/manage.sh status                    # the timer, the heartbeat, the last request
```

To turn the agent off: `sudo systemctl disable --now alliance-manager-update.timer`. Updates keep
that choice; the Admin page then says the agent is not running, and updates go back to the
terminal.

On the Admin page, **About this install and updates** shows the agent (active, and when it was
last seen; or stale, or not installed) and, when a newer release is out and the install is one the
app can vouch for — a release build, `APP_VERSION` pinned to it, the default `DATABASE_PATH`, a
fresh agent — an **Install** button; otherwise the reason it cannot. The confirmation says what
happens: a backup, the download, about a minute of downtime, the health check, and the automatic
return on failure. For a **major** it leads with what differs: the release changes files on the
server, and may need a step only you can take, so read its notes first. Any level can be installed
this way.

**While an update is being checked, the app accepts no changes.** Every save answers *update in
progress* and a banner says so; logging in still works, and an open document in the editor cannot
be saved until the update ends. This is what lets the agent restore the pre-update database after
a failed migration without losing anything: nothing was accepted after the backup. If the agent
stops refreshing its hold for two minutes, the app gives up waiting, records that it did, and
accepts changes again; the agent will then never restore the database for that request on its
own. **Test the host agent** sends a request that changes nothing, to prove the round trip.

#### Install is refused: "… is group- or world-writable (mode 775)"

A known issue up to and including v3.1.0. The agent refuses to run an install whose directories
— the install directory, `scripts/` and `scripts/lib/`, or any directory above it — can be written
by anyone but the owner, as `sudo` does. On hosts whose default umask is `0002` (Ubuntu's default,
with a private group per user), the install steps create exactly those directories with mode
`775`, so every request from the Admin page ends with this refusal, and nothing on the server
changes. **Test the host agent** still succeeds: it does not run this check.

The fix needs no new release. In the install directory:

```bash
chmod 755 . scripts scripts/lib
```

then press **Install** again. `journalctl -u alliance-manager-update` names the path it refused.
A new install avoids it by running `umask 022` before creating the directory and unpacking the
host files (see the [README](../README.md#quick-install)). A later release will create these
directories with safe modes and report a refusal on the Admin page before you press Install.

#### Install is refused: "the collabora image … is not on this host"

A known issue up to and including v3.2.0. Before an install moves anything, the agent tags every
image of the running stack so a rollback can return to it. Collabora is pinned by digest, so that
tag is its only one. When a request ends, the agent removes its tags. On Docker's classic image
store (`overlay2`, `docker info` shows no `containerd` snapshotter), removing an image's last tag
also drops its digest. The Collabora container keeps running, but nothing names its image any
more. The next request then cannot tag it and is refused before anything changes. So the first
Admin-page update works, and every later one is refused until this is done:

```bash
docker pull "$(docker compose config --images | grep collabora)"
```

Run it in the install directory, then press **Install** again. The layers are already on the host,
so the pull only restores the name: nothing restarts. It is needed after each successful Admin-page
update until a later release fixes the agent. A terminal update (`./scripts/manage.sh update`)
pulls every image it installs, so it is not affected.

### Recovering from a failed update

A failed update from the Admin page ends in one of three ways, and the Admin page says which:

- **Rolled back** — the previous release is running again and healthy. Nothing to do; the
  request's backup, bundle and log are kept (the newest three failed requests, for 30 days) in
  case you want to look. `./scripts/manage.sh status` lists them.
- **Failed, the app running** — the agent could not prove the previous release healthy, or
  nothing had changed yet. Look at the log, then run `./scripts/manage.sh status`; a terminal
  `./scripts/manage.sh update --version <release>` moves it to any release you choose.
- **Failed, the app stopped** — the database had been restored to the pre-update backup, and then
  putting the previous release's files back failed. The agent leaves the app stopped, because
  starting the newer release on the older database would migrate it again, and says so on every
  tick (`journalctl -u alliance-manager-update`). To finish by hand, from the install directory:

  ```bash
  ./scripts/manage.sh status                         # which release the files are on now
  ./scripts/manage.sh update --version <previous>    # the release you were on before
  ```

  The pre-update database backup is `/var/backups/lastwar/pinned/<id>.db` if you need it.
  Once the install is running, clear the agent's record so the Admin page offers updates again:
  `sudo rm /var/lib/alliance-manager/default/journal`.

### Where installs are recorded

Each install is recorded in `/etc/alliance-manager/installs.d/<name>.conf` — a root-owned file
holding `APP_DIR="…"` (and, from v3.0.0, `OWNER_UID="…"`, the account the install belongs to).
The first install is named `default`. The nightly backup and the host update agent read this
list, which is how they find the install wherever you put it.

### Moving an install

Moving the install directory is a manual procedure, and the data moves with it:

```bash
cd <install directory>
sudo docker compose down                 # down, not stop — see below
sudo sqlite3 data/alliance.db ".backup /tmp/before.db" && sha256sum /tmp/before.db
sudo find uploads -type f | sort > /tmp/uploads-before.txt
sudo mv <install directory> <new directory>
sudoedit /etc/alliance-manager/installs.d/default.conf   # set APP_DIR="<new directory>"
cd <new directory> && sudo docker compose up -d
```

Then compare a fresh `.backup` checksum and an `uploads/` listing with the ones taken before.
Use `down`, not `stop`: both services have fixed container names, so a stopped container left
behind collides with the one Compose creates from the new directory. `down` without `-v` removes
the containers only — `data/`, `uploads/` and the OCR model volume stay.

### Migrating an install from before v2.0.0

Installs from before v2.0.0 are a git clone of the repository (or a copy of one), updated by
`update.sh` pulling `main`. v2.0.0 converts them, in place, to versioned host files. **Do not
run `update.sh` for this release.** From the install directory:

```bash
git pull
./scripts/manage.sh migrate
```

`migrate` takes a backup, then removes the files git delivered that a host does not need (the
source code, templates, docs), keeps everything git never delivered (`.env`, `data/`, `uploads/`,
your own files), writes the v2.0.0 host files, pins the image to v2.0.0, registers the install,
installs the nightly backup helper, and finally removes `.git`, so a `git pull` from habit can no
longer drag unreleased files over released ones. Your data is not moved. Any step failing puts
the clone back as it was.

**If `git pull` refuses because of local changes**, run `git stash` first, then `git pull`.
`migrate` saves uncommitted changes, every stash and any local commits as patch files under
`/var/backups/lastwar/migrate_<timestamp>/`, says where, and asks you to re-run with `--yes`
before it replaces the files they touched.

**An install with no `.git`** (copied over with SCP rather than cloned) cannot `git pull`.
Download the v2.0.0 asset into the install directory, take its `scripts/` out, and migrate:

```bash
curl -fsSL -o host-files.tar.gz \
  https://github.com/shodiwarmic/lastwar-alliance-manager/releases/download/v2.0.0/host-files.tar.gz
tar -xzf host-files.tar.gz scripts/ && rm host-files.tar.gz
./scripts/manage.sh migrate
```

With no history to say which files were delivered, nothing is removed: the new files are laid
over the old ones.

**An install still running the pre-Docker `lastwar.service`** (data under `/var/lib/lastwar`) is
not converted by this release. Convert it to Docker with the v1.1.0 scripts first — their update
copies `/var/lib/lastwar` into the Docker install — then follow the steps above:

```bash
git checkout v1.1.0 && ./scripts/update.sh
git checkout main && git pull && ./scripts/manage.sh migrate
```

`migrate` recognises this case and says so rather than proceeding.

`TRUSTED_PROXY_COUNT=1` is written during the migration when `.env` has `PRODUCTION=true`
(behind a proxy). The ports are published on `127.0.0.1` from v2.0.0 on; a LAN install reached
without a proxy needs `BIND_ADDR=0.0.0.0` in `.env`.
