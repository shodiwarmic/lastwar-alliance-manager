# Quick Production Setup Guide

## One-Command Installation (Debian/Ubuntu)

```bash
git clone [https://github.com/shodiwarmic/lastwar-alliance-manager.git](https://github.com/shodiwarmic/lastwar-alliance-manager.git) /opt/lastwar
cd /opt/lastwar
chmod +x install.sh
sudo ./install.sh
```

The script will automatically:
- ✅ Install Docker and Docker Compose
- ✅ Create persistent data directories
- ✅ Generate secure session/encryption keys and a `.env` file
- ✅ Build the Go application and Collabora containers via Docker Compose
- ✅ Install Caddy with dual-domain routing and automatic SSL
- ✅ Apply strict Content-Security-Policy headers for document security
- ✅ Setup firewall (UFW) and automated database backups

---

## Manual Quick Start (Docker Compose)

If you prefer to skip the script and spin it up manually:

### 1. Prerequisites
- **DNS**: Two domains pointing to your server IP (e.g., `app.domain.com` and `collabora.domain.com`)
- **Firewall**: Ports 80 and 443 open
- **Software**: Git, Docker, and Docker Compose installed

### 2. Prepare Environment
```bash
git clone [https://github.com/shodiwarmic/lastwar-alliance-manager.git](https://github.com/shodiwarmic/lastwar-alliance-manager.git) /opt/lastwar
cd /opt/lastwar

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
EOF
```

### 3. Start the Application Stack
```bash
docker compose up -d --build
```

### 4. Setup Reverse Proxy (Caddy - Recommended)
```bash
# Install Caddy
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf '[https://dl.cloudsmith.io/public/caddy/stable/gpg.key](https://dl.cloudsmith.io/public/caddy/stable/gpg.key)' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf '[https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt](https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt)' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install caddy

# Configure proxy and security headers
cat << 'EOF' | sudo tee /etc/caddy/Caddyfile
app.yourdomain.com {
    reverse_proxy localhost:8080
    header {
        X-Frame-Options "DENY"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
    }
}
collabora.yourdomain.com {
    reverse_proxy localhost:9980
    header {
        Content-Security-Policy "frame-ancestors [https://app.yourdomain.com](https://app.yourdomain.com)"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
    }
}
EOF

sudo systemctl restart caddy
```

---

## Essential Commands

### Container Management
Run these commands from inside your `/opt/lastwar` directory:
```bash
docker compose ps                 # Check container status
docker compose logs -f            # View real-time logs for all services
docker compose logs -f app        # View only Go backend logs
docker compose restart            # Restart the stack
docker compose down               # Stop and remove containers
```

### Backups
```bash
# Manual database backup (from host machine)
sqlite3 /opt/lastwar/data/alliance.db ".backup '/var/backups/lastwar/alliance_$(date +%Y%m%d_%H%M%S).db'"

# Restore from backup
sudo cp /var/backups/lastwar/alliance_YYYYMMDD_HHMMSS.db /opt/lastwar/data/alliance.db
docker compose restart app
```

### Updates
```bash
cd /opt/lastwar
sudo ./update.sh
```

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