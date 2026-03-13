#!/bin/bash
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Last War Alliance Manager - Update Script${NC}"
echo "=========================================="
echo ""

if [[ $EUID -eq 0 ]]; then
   echo -e "${RED}This script should NOT be run as root${NC}"
   exit 1
fi

APP_NAME="lastwar"
APP_USER="lastwar"
APP_DIR="/opt/lastwar"
DATA_DIR="/var/lib/lastwar"
BACKUP_DIR="/var/backups/lastwar"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    DISTRO_CODENAME=$VERSION_CODENAME
else
    echo -e "${RED}Cannot detect OS. This script requires Debian or Ubuntu.${NC}"
    exit 1
fi

if [ "$OS" != "debian" ] && [ "$OS" != "ubuntu" ]; then
    echo -e "${RED}Unsupported OS: $OS. This script requires Debian or Ubuntu.${NC}"
    exit 1
fi

if [ ! -d "$APP_DIR" ]; then
    echo -e "${RED}Application directory not found: $APP_DIR${NC}"
    exit 1
fi

# Upgrade legacy environments
if sudo test -f "$APP_DIR/.env"; then
    set -a
    source /dev/stdin <<< "$(sudo cat $APP_DIR/.env)"
    set +a
fi

if [ -z "$APP_DOMAIN" ]; then
    APP_DOMAIN=$(grep -oP '^([a-zA-Z0-9.-]+)\s*\{' /etc/caddy/Caddyfile 2>/dev/null | head -n 1 | tr -d ' {')
    if [ -z "$APP_DOMAIN" ]; then
        APP_DOMAIN=$(grep -oP 'server_name\s+\K[^; ]+' /etc/nginx/sites-available/lastwar 2>/dev/null | head -n 1)
    fi
fi

if [ -z "$COLLABORA_DOMAIN" ]; then
    echo ""
    echo -e "${YELLOW}Document Sharing Upgrade Detected${NC}"
    read -p "Enter your new Collabora domain name (default: collabora.$APP_DOMAIN): " NEW_COL_DOMAIN
    COLLABORA_DOMAIN=${NEW_COL_DOMAIN:-collabora.$APP_DOMAIN}

    echo ""
    echo -e "${RED}!!! DNS CONFIGURATION REQUIRED !!!${NC}"
    echo -e "${YELLOW}Before continuing, your DNS provider MUST have an A-Record pointing to this server for:${NC}"
    echo " -> $COLLABORA_DOMAIN"
    read -p "Press Enter to confirm DNS is configured, or Ctrl+C to abort..."
    
    echo "APP_DOMAIN=$APP_DOMAIN" | sudo tee -a $APP_DIR/.env >/dev/null
    echo "COLLABORA_DOMAIN=$COLLABORA_DOMAIN" | sudo tee -a $APP_DIR/.env >/dev/null
    
    if [ -f "/etc/caddy/Caddyfile" ]; then
        if ! grep -q "$COLLABORA_DOMAIN" /etc/caddy/Caddyfile; then
            sudo tee -a /etc/caddy/Caddyfile > /dev/null <<EOF

$COLLABORA_DOMAIN {
    encode gzip
    reverse_proxy localhost:9980
}
EOF
            sudo systemctl restart caddy
        fi
    else
        echo -e "${YELLOW}Warning: Please manually add $COLLABORA_DOMAIN to your Nginx configuration.${NC}"
    fi
fi

UPDATE_METHOD=""
if [ -d "$APP_DIR/.git" ]; then
    UPDATE_METHOD="git"
elif [ "$1" = "--git" ]; then
    UPDATE_METHOD="git"
else
    UPDATE_METHOD="scp"
fi

if [ "$1" = "--setup-git" ]; then
    if [ -z "$2" ]; then exit 1; fi
    REPO_URL="$2"
    sudo mkdir -p $BACKUP_DIR
    sudo tar -czf $BACKUP_DIR/app_before_git_$TIMESTAMP.tar.gz -C $APP_DIR --exclude='.git' .
    cd $APP_DIR
    sudo -u $APP_USER git init
    sudo -u $APP_USER git remote add origin $REPO_URL
    sudo -u $APP_USER git fetch
    sudo -u $APP_USER git reset --hard origin/main || sudo -u $APP_USER git reset --hard origin/master
    exit 0
fi

echo -e "${YELLOW}[1/8] Creating backup...${NC}"
sudo mkdir -p $BACKUP_DIR
sudo tar -czf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C $APP_DIR --exclude='.git' .
sudo sqlite3 $DATA_DIR/alliance.db ".backup '$BACKUP_DIR/db_$TIMESTAMP.db'"

echo -e "${YELLOW}[2/8] Stopping application...${NC}"
sudo systemctl stop $APP_NAME

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo -e "${YELLOW}[3/8] Updating system dependencies & Docker...${NC}"
sudo apt update
sudo apt install -y sqlite3 tesseract-ocr tesseract-ocr-all libtesseract-dev libleptonica-dev

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

# Ensure Docker daemon is running before querying it
if ! sudo systemctl is-active --quiet docker; then
    echo "Starting Docker service..."
    sudo systemctl enable --now docker
fi

if ! sudo docker ps | grep -q "collabora/code"; then
    echo "Starting Collabora container..."
    sudo docker run -t -d -p 9980:9980 \
        -e "domain=$APP_DOMAIN" \
        -e "aliasgroup1=https://$APP_DOMAIN:443" \
        -e "extra_params=--o:ssl.enable=false --o:ssl.termination=true" \
        --restart always \
        --cap-add MKNOD \
        collabora/code
fi

if [ "$UPDATE_METHOD" = "git" ]; then
    echo -e "${YELLOW}[4/8] Pulling latest changes from git...${NC}"
    cd $APP_DIR
    sudo -u $APP_USER git stash push -m "Auto-stash before update $TIMESTAMP"
    sudo -u $APP_USER git pull origin main || sudo -u $APP_USER git pull origin master
    if sudo -u $APP_USER git stash list | grep -q "Auto-stash"; then
        sudo -u $APP_USER git stash pop || true
    fi
else
    echo -e "${YELLOW}[4/8] Copying new files...${NC}"
    sudo cp "$SCRIPT_DIR/"*.go $APP_DIR/
    sudo cp "$SCRIPT_DIR/go.mod" $APP_DIR/ 2>/dev/null || true
    sudo cp "$SCRIPT_DIR/go.sum" $APP_DIR/ 2>/dev/null || true
    sudo cp "$SCRIPT_DIR/"*.html $APP_DIR/ 2>/dev/null || true
    if [ -d "$SCRIPT_DIR/static" ]; then sudo cp -r "$SCRIPT_DIR/static/"* $APP_DIR/static/; fi
    if [ -d "$SCRIPT_DIR/templates" ]; then sudo cp -r "$SCRIPT_DIR/templates/"* $APP_DIR/templates/; fi
fi

export PATH=$PATH:/usr/local/go/bin
echo -e "${YELLOW}[6/8] Building application...${NC}"
cd $APP_DIR
if [ -f "go.mod" ]; then
    sudo -u $APP_USER env PATH=$PATH:/usr/local/go/bin go mod download
fi
sudo -u $APP_USER env PATH=$PATH:/usr/local/go/bin go build -o alliance-manager .

if [ ! -f "alliance-manager" ]; then
    sudo tar -xzf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C $APP_DIR
    sudo systemctl start $APP_NAME
    exit 1
fi

sudo chown -R $APP_USER:$APP_USER $APP_DIR/alliance-manager
sudo chmod +x $APP_DIR/alliance-manager

echo -e "${YELLOW}[8/8] Starting application...${NC}"
sudo systemctl start $APP_NAME
sleep 2

if sudo systemctl is-active --quiet $APP_NAME; then
    echo -e "${GREEN}✓ Application is running${NC}"
else
    sudo systemctl stop $APP_NAME
    sudo tar -xzf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C $APP_DIR
    sudo systemctl start $APP_NAME
    exit 1
fi

cd $BACKUP_DIR
ls -t app_*.tar.gz 2>/dev/null | tail -n +11 | xargs -r sudo rm
ls -t db_*.db 2>/dev/null | tail -n +11 | xargs -r sudo rm

echo -e "${GREEN}Update Complete!${NC}"