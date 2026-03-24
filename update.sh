#!/bin/bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Last War Alliance Manager - Docker Update Script${NC}"
echo "================================================="
echo ""

if [[ $EUID -eq 0 ]]; then
   echo -e "${RED}This script should NOT be run as root${NC}"
   exit 1
fi

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/var/backups/lastwar"
sudo mkdir -p $BACKUP_DIR

# Determine update method (Git vs SCP)
UPDATE_METHOD=""
if [ -d ".git" ]; then
    UPDATE_METHOD="git"
elif [ "$1" = "--git" ]; then
    UPDATE_METHOD="git"
else
    UPDATE_METHOD="scp"
fi

if [ "$1" = "--setup-git" ]; then
    if [ -z "$2" ]; then echo "Missing repo URL"; exit 1; fi
    REPO_URL="$2"
    sudo tar -czf $BACKUP_DIR/app_before_git_$TIMESTAMP.tar.gz -C . --exclude='.git' .
    git init
    git remote add origin $REPO_URL
    git fetch
    git reset --hard origin/main || git reset --hard origin/master
    exit 0
fi

echo -e "${YELLOW}[1/6] Creating backups...${NC}"
sudo tar -czf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C . --exclude='.git' .
if [ -f "./data/alliance.db" ]; then
    sudo sqlite3 ./data/alliance.db ".backup '$BACKUP_DIR/db_$TIMESTAMP.db'"
elif [ -f "/var/lib/lastwar/alliance.db" ]; then
    sudo sqlite3 /var/lib/lastwar/alliance.db ".backup '$BACKUP_DIR/db_$TIMESTAMP.db'"
fi

# --- LEGACY MIGRATION BLOCK ---
# This block runs only once to transition a server from systemd to Docker
if systemctl is-active --quiet lastwar.service 2>/dev/null || [ -d "/opt/lastwar" ]; then
    echo -e "${YELLOW}Legacy bare-metal deployment detected. Migrating to Docker...${NC}"
    
    # 1. Stop legacy service
    if systemctl is-active --quiet lastwar.service 2>/dev/null; then
        sudo systemctl stop lastwar.service
        sudo systemctl disable lastwar.service
        sudo rm -f /etc/systemd/system/lastwar.service
        sudo systemctl daemon-reload
    fi

    # 2. Kill standalone Collabora container if it exists
    if sudo docker ps | grep -q "collabora/code"; then
        sudo docker stop $(sudo docker ps -q --filter ancestor=collabora/code)
    fi

    # 3. Create Docker volume directories
    mkdir -p ./data ./uploads

    # 4. Migrate Database
    if [ -f "/var/lib/lastwar/alliance.db" ]; then
        sudo cp /var/lib/lastwar/alliance.db ./data/
        sudo chown $USER:$USER ./data/alliance.db
    fi

    # 5. Migrate Files
    if [ -d "/var/lib/lastwar/files" ] && [ "$(ls -A /var/lib/lastwar/files)" ]; then
        sudo cp -r /var/lib/lastwar/files/* ./uploads/
        sudo chown -R $USER:$USER ./uploads/
    fi

    # 6. Migrate Env File
    if [ -f "/opt/lastwar/.env" ]; then
        sudo cp /opt/lastwar/.env ./.env
        sudo chown $USER:$USER ./.env
        
        # Ensure new Docker paths are set in the .env
        sed -i 's|DATABASE_PATH=.*|DATABASE_PATH=/app/data/alliance.db|g' ./.env
        sed -i 's|STORAGE_PATH=.*|STORAGE_PATH=/app/uploads|g' ./.env
    fi

    # 7. Clean up orphaned user (Security)
    if id "lastwar" &>/dev/null; then
        sudo userdel lastwar || true
    fi
fi
# --- END MIGRATION BLOCK ---

echo -e "${YELLOW}[2/6] Updating code...${NC}"
if [ "$UPDATE_METHOD" = "git" ]; then
    git stash push -m "Auto-stash before update $TIMESTAMP"
    git pull origin main || git pull origin master
    if git stash list | grep -q "Auto-stash"; then
        git stash pop || true
    fi
else
    echo "Assuming files were copied via SCP."
fi

# Ensure .env exists and has required encryption keys
if [ ! -f ".env" ]; then
    echo "Generating secure .env file..."
    echo "SESSION_KEY=$(openssl rand -hex 32)" > .env
    echo "CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -hex 32)" >> .env
    echo "DATABASE_PATH=/app/data/alliance.db" >> .env
    echo "STORAGE_PATH=/app/uploads" >> .env
    echo "PORT=8080" >> .env
else
    # Inject GCP Encryption Key for existing users upgrading to this version
    if ! grep -q "CREDENTIAL_ENCRYPTION_KEY=" .env; then
        echo -e "${YELLOW}Injecting missing CREDENTIAL_ENCRYPTION_KEY into .env...${NC}"
        echo "CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -hex 32)" >> .env
    fi
fi

echo -e "${YELLOW}[3/6] Cleaning Go Modules...${NC}"
# Use a temporary docker container to run go mod tidy and strip out Tesseract
sudo docker run --rm -v "$PWD":/usr/src/app -w /usr/src/app golang:1.25-bookworm go mod tidy

echo -e "${YELLOW}[4/6] Updating Docker...${NC}"
if ! command -v docker &> /dev/null; then
    sudo apt-get update
    sudo apt-get install -y ca-certificates curl
    sudo install -m 0755 -d /etc/apt/keyrings
    sudo curl -fsSL https://download.docker.com/linux/$OS/gpg -o /etc/apt/keyrings/docker.asc
    sudo chmod a+r /etc/apt/keyrings/docker.asc
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$OS $DISTRO_CODENAME stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
    sudo apt-get update
    sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

if ! sudo systemctl is-active --quiet docker; then
    sudo systemctl enable --now docker
fi

echo -e "${YELLOW}Cleaning up legacy standalone containers...${NC}"
# Find collabora containers, exclude those managed by Compose, and cleanly remove them
sudo docker ps -a --filter "ancestor=collabora/code" --format '{{.ID}}\t{{.Labels}}' | grep -v "com.docker.compose.project" | awk '{print $1}' | xargs -r sudo docker stop
sudo docker ps -a --filter "ancestor=collabora/code" --format '{{.ID}}\t{{.Labels}}' | grep -v "com.docker.compose.project" | awk '{print $1}' | xargs -r sudo docker rm

echo -e "${YELLOW}Checking reverse proxy security configurations...${NC}"
if [ -f "/etc/caddy/Caddyfile" ]; then
    # Check if the modern CSP frame-ancestors rule is missing
    if ! grep -q "frame-ancestors" /etc/caddy/Caddyfile; then
        echo -e "${YELLOW}Legacy Caddyfile detected. Upgrading security headers...${NC}"
        
        # Load environment variables so we know the domains
        set -a; source .env; set +a
        
        # Backup the existing configuration just in case
        sudo cp /etc/caddy/Caddyfile "/etc/caddy/Caddyfile.backup_$(date +%Y%m%d_%H%M%S)"
        
        sudo tee /etc/caddy/Caddyfile > /dev/null <<EOF
$APP_DOMAIN {
    reverse_proxy localhost:8080
    encode gzip
    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
}

$COLLABORA_DOMAIN {
    encode gzip
    reverse_proxy localhost:9980
    header {
        X-Content-Type-Options "nosniff"
        Content-Security-Policy "frame-ancestors https://$APP_DOMAIN"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
}
EOF
        sudo systemctl reload caddy
        echo -e "${GREEN}Caddyfile upgraded and reloaded successfully! Backup saved.${NC}"
    else
        echo -e "${GREEN}Caddy security headers are already up to date.${NC}"
    fi
fi

echo -e "${YELLOW}[5/6] Building and starting Docker containers...${NC}"
sudo docker compose pull
# Force a clean build to ensure Tesseract dependencies are purged from the image layers
sudo docker compose build --no-cache
sudo docker compose up -d
sudo docker image prune -f

echo -e "${YELLOW}[6/6] Cleaning up old backups...${NC}"
cd $BACKUP_DIR
ls -t app_*.tar.gz 2>/dev/null | tail -n +11 | xargs -r sudo rm
ls -t db_*.db 2>/dev/null | tail -n +11 | xargs -r sudo rm

echo -e "${GREEN}Update Complete! Application is running in Docker.${NC}"