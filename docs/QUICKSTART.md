# Quick Production Setup Guide

## One-Command Installation (Debian/Ubuntu)

```bash
mkdir -p ~/alliance-manager && cd ~/alliance-manager      # any directory you like
curl -fsSL -o host-files.tar.gz \
  https://github.com/shodiwarmic/lastwar-alliance-manager/releases/latest/download/host-files.tar.gz
tar -xzf host-files.tar.gz && rm host-files.tar.gz
./scripts/install.sh
```

The script will automatically:
- ✅ Check the host first (OS, Docker, free ports, DNS, disk, memory) and offer to fix what it can
- ✅ Install Docker and Docker Compose
- ✅ Create persistent data directories
- ✅ Generate secure session/encryption keys and a `.env` file
- ✅ Pull the pre-built application and Collabora containers, pinned to the release you downloaded
- ✅ Install Caddy with dual-domain routing and automatic SSL
- ✅ Apply strict Content-Security-Policy headers for document security
- ✅ Set up the firewall (UFW) and nightly database backups
- ✅ Print the one-time setup key for creating the first administrator

Unattended: `./scripts/install.sh --non-interactive --domain app.yourdomain.com` (see `--help`).

---

## Manual Quick Start (Docker Compose)

If you prefer to skip the script and spin it up manually:

### 1. Prerequisites
- **DNS**: Two domains pointing to your server IP (e.g., `app.domain.com` and `collabora.domain.com`)
- **Firewall**: Ports 80 and 443 open
- **Software**: Docker and Docker Compose installed

### 2. Prepare Environment
```bash
mkdir -p ~/alliance-manager && cd ~/alliance-manager      # any directory you like
curl -fsSL -o host-files.tar.gz \
  https://github.com/shodiwarmic/lastwar-alliance-manager/releases/latest/download/host-files.tar.gz
tar -xzf host-files.tar.gz && rm host-files.tar.gz

# Create volume directories
mkdir -p ./data ./uploads

# Generate environment file
cat > .env << EOF
DATABASE_PATH=/app/data/alliance.db
STORAGE_PATH=/app/uploads
SESSION_KEY=$(openssl rand -hex 32)
CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -hex 32)
PRODUCTION=true
HTTPS=true
APP_DOMAIN=app.yourdomain.com
COLLABORA_DOMAIN=collabora.yourdomain.com
TRUSTED_ORIGINS=localhost:8080, 127.0.0.1:8080
APP_VERSION=$(cat HOST_FILES_VERSION)
HOST_FILES_VERSION=$(cat HOST_FILES_VERSION)
EOF
```

For the local OCR sidecar instead of Google Cloud Vision, also add `OCR_BACKEND_MODE=local` and
`COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml` before starting.

### 3. Start the Application Stack
```bash
sudo docker compose up -d
```

Then read the one-time setup key and open `https://app.yourdomain.com/setup` to create the first
administrator — there is no default account:

```bash
sudo cat data/setup-key
```

### 4. Setup Reverse Proxy (Caddy - Recommended)
```bash
# Install Caddy
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy

# Render the shipped template with your two domains
sed -e 's|__APP_DOMAIN__|app.yourdomain.com|g' \
    -e 's|__COLLABORA_DOMAIN__|collabora.yourdomain.com|g' \
    deploy/Caddyfile | sudo tee /etc/caddy/Caddyfile >/dev/null
sudo systemctl restart caddy
```

---

## Essential Commands

### Container Management
Run these commands from inside your install directory:
```bash
docker compose ps                 # Check container status
docker compose logs -f            # View real-time logs for all services
docker compose logs -f app        # View only Go backend logs
docker compose restart            # Restart the stack
docker compose down               # Stop and remove containers
```

### Backups
```bash
# Manual database backup of every registered install (what the nightly cron job runs)
sudo /usr/local/bin/backup-lastwar.sh

# Restore from a backup — from your install directory, with the app stopped
sudo docker compose stop alliance-manager
sudo cp /var/backups/lastwar/nightly_default_YYYYMMDD_HHMMSS.db data/alliance.db
sudo docker compose start alliance-manager
```

### Updates
```bash
cd <install directory>
./scripts/manage.sh update                     # newest release — host files and image together
./scripts/manage.sh update --version v2.0.0    # a specific release (also how to roll back)
./scripts/manage.sh status                     # what is running
```

**Upgrading an install from before v2.0.0** (a git clone): don't run `update.sh` for this one —
from the install directory run `git pull`, then `./scripts/manage.sh migrate`. See
[DEPLOYMENT.md → Migrating an install from before v2.0.0](DEPLOYMENT.md#migrating-an-install-from-before-v200)
for local changes, SCP copies and pre-Docker installs.

---

## Troubleshooting

### App won't start or throws 502 Bad Gateway
```bash
# Check if the Go container is crashing
docker compose logs app

# Check if ports are successfully mapped to the host
sudo ss -tlnp | grep -E '(8080|9980)'
```

### Document Editor won't load
```bash
# Verify Collabora is healthy
docker compose logs collabora

# Check if Collabora domain is reachable externally
curl -I [https://collabora.yourdomain.com/hosting/discovery](https://collabora.yourdomain.com/hosting/discovery)
```

### CSRF "Forbidden" Errors
If you cannot log in or save settings, ensure your `TRUSTED_ORIGINS` in your `.env` file includes the exact domain or IP you are using to access the site, including the port (e.g., `TRUSTED_ORIGINS=192.168.1.50:8080`).

---

## Quick Health Check

Run this to verify your Docker stack is operating correctly:

```bash
echo "=== Container Status ==="
docker compose ps

echo -e "\n=== Port Check ==="
sudo ss -tlnp | grep -E '(8080|9980|80|443)'

echo -e "\n=== Database Check ==="
sqlite3 ./data/alliance.db "SELECT COUNT(*) FROM members;"

echo -e "\n=== Recent Application Logs ==="
docker compose logs --tail=20 app
```