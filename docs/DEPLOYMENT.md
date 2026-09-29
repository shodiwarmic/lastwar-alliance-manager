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
| `APP_VERSION` | `latest` | Which published image this install runs (`v1.2.3`, the moving `v1.2`, `latest`, or `edge`). `scripts/install.sh` and `scripts/update.sh` write the current release tag here for you; set it by hand only to pin to or roll back to a specific release. `latest` means the latest *release*; `edge` is built from `main`. See [RELEASING.md](RELEASING.md). |
| `PORT` | `8080` | Port the Go application listens on inside its container. Change it only if you also change the published port in `docker-compose.yml`. |
| `COLLABORA_PORT` | *(unset)* | Explicit port for the Collabora document server, appended to `COLLABORA_DOMAIN` when building WOPI URLs. Needed only when Collabora is reached on a non-standard port rather than through the reverse proxy. |
| `TRUSTED_PROXY_COUNT` | `1` when `PRODUCTION=true`, else `0` | How many reverse proxies sit in front of the app. The client address every login rate limit keys on is read that many entries from the right of `X-Forwarded-For`; entries further left were sent by the client and are ignored. `0` ignores the header and uses the TCP peer. Set `2` if a CDN or load balancer sits in front of your Caddy or nginx. On an unproxied install running `PRODUCTION=true`, set `0` — otherwise a client could choose its own rate-limit bucket. |
| `BIND_ADDR` | `127.0.0.1` | Read by `docker-compose.yml`, not by the app: the address ports 8080 and 9980 are published on. The default keeps them reachable only through the reverse proxy on the same host (Docker's published ports bypass `ufw`). Set `0.0.0.0` for a LAN or development install with no proxy. |
| `OCR_BACKEND_MODE` | `cloud` | Set to `local` to use the bundled PaddleOCR sidecar instead of Google Cloud Vision. Also requires `COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml`. `scripts/install.sh` and `scripts/update.sh` set both for you if you opt in. See [IMAGE_RECOGNITION.md](IMAGE_RECOGNITION.md). |

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
| `db_<timestamp>.db` and `app_<timestamp>.tar.gz` | Before every update | The database, and the install directory without `data/` (so including `uploads/` — the alliance's files) | The newest 10 of each |

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

We strongly recommend using the included `scripts/update.sh` script. It pulls the latest code,
moves your `APP_VERSION` pin to the newest release, downloads that image, and checks your proxy
configuration for security compliance.

```bash
cd /opt/lastwar
./scripts/update.sh
```

If the script cannot reach the GitHub releases API it leaves an existing pin exactly as it is
rather than silently moving your install onto something else.

### Updating manually

An install runs the image named by `APP_VERSION` in `.env`, so a manual update is a change to
that line followed by a pull:

```bash
cd /opt/lastwar
git pull                        # brings the compose file and scripts up to date
nano .env                       # set APP_VERSION=v1.2.3 (or latest)
docker compose pull
docker compose up -d
```

> **There is nothing to build.** The production compose file has no `build:` key — the image is
> pre-built and published for you, so `docker compose build` does nothing here. Building from
> source is a development workflow and needs the dev override
> (`deploy/docker-compose.override.yml.example`), which ignores `APP_VERSION` by design.

### Pinning and rolling back

The same mechanism covers both. To hold an install on a known-good release, or to go back to
one after a bad update, set the tag and pull:

```bash
# Roll back to a specific release
sed -i 's/^APP_VERSION=.*/APP_VERSION=v1.2.3/' .env
docker compose pull && docker compose up -d
```

Published tags are `vX.Y.Z` (exact release), `vX.Y` (moves with its patches), `latest` (the
newest release) and `edge` (built from `main` — development, not a release). What each release
level promises about an update is set out in [RELEASING.md](RELEASING.md).

Confirm which build is actually running afterwards on **Admin → Security & API → About this
install**, or without logging in:

```bash
docker compose logs alliance-manager | head -n 1
```