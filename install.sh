#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Last War Alliance Manager - Docker Installation Script${NC}"
echo "========================================================="
echo ""

# Check if running as root
if [[ $EUID -eq 0 ]]; then
   echo -e "${RED}This script should NOT be run as root${NC}"
   echo "Please run as a regular user with sudo privileges"
   exit 1
fi

command -v sudo >/dev/null 2>&1 || { echo -e "${RED}sudo is required but not installed${NC}"; exit 1; }

# Capture the exact path where the app is being installed for the backup script later
APP_DIR=$(pwd)

# Detect OS
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

# Ask for domains
read -p "Enter your main app domain name (e.g., app.example.com): " DOMAIN
if [ -z "$DOMAIN" ]; then
   echo -e "${RED}App domain name is required${NC}"
   exit 1
fi

read -p "Enter your Collabora domain name (default: collabora.$DOMAIN): " COLLABORA_DOMAIN
COLLABORA_DOMAIN=${COLLABORA_DOMAIN:-collabora.$DOMAIN}

echo ""
echo -e "${RED}!!! DNS CONFIGURATION REQUIRED !!!${NC}"
echo -e "${YELLOW}Before continuing, your DNS provider MUST have A-Records pointing to this server's IP for:${NC}"
echo " 1. $DOMAIN"
echo " 2. $COLLABORA_DOMAIN"
echo "If these are not set, the reverse proxy will fail to start and SSL generation will break."
read -p "Press Enter to confirm your DNS is configured, or Ctrl+C to abort..."

echo ""
echo "Choose reverse proxy:"
echo "1) Caddy (Recommended - Automatic HTTPS)"
echo "2) Nginx (Manual Let's Encrypt setup)"
read -p "Enter choice [1-2]: " PROXY_CHOICE

echo ""
echo -e "${YELLOW}Starting installation...${NC}"
echo ""

echo -e "${GREEN}[1/6] Updating system packages...${NC}"
sudo apt update

echo -e "${GREEN}[2/6] Installing dependencies...${NC}"
sudo apt install -y curl wget git ufw fail2ban sqlite3

echo -e "${GREEN}[3/6] Installing Docker & Docker Compose...${NC}"
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

echo -e "${GREEN}[4/6] Creating environment and directories...${NC}"
mkdir -p ./data ./uploads

if [ ! -f ".env" ]; then
    echo "Generating secure .env file..."
    SESSION_KEY=$(openssl rand -hex 32)
    CREDENTIAL_ENCRYPTION_KEY=$(openssl rand -hex 32)
    cat > .env <<EOF
DATABASE_PATH=/app/data/alliance.db
STORAGE_PATH=/app/uploads
SESSION_KEY=$SESSION_KEY
CREDENTIAL_ENCRYPTION_KEY=$CREDENTIAL_ENCRYPTION_KEY
PRODUCTION=true
HTTPS=true
PORT=8080
APP_DOMAIN=$DOMAIN
COLLABORA_DOMAIN=$COLLABORA_DOMAIN
EOF
fi

echo -e "${GREEN}[5/6] Configuring firewall and proxy...${NC}"
sudo ufw --force enable
sudo ufw allow 22/tcp comment 'SSH'
sudo ufw allow 80/tcp comment 'HTTP'
sudo ufw allow 443/tcp comment 'HTTPS'

if [ "$PROXY_CHOICE" = "1" ]; then
    echo "Installing Caddy..."
    sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
    sudo apt update
    sudo apt install -y caddy
    
    sudo tee /etc/caddy/Caddyfile > /dev/null <<EOF
$DOMAIN {
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
        # X-Frame-Options is omitted here because we MUST allow embedding.
        # Instead, we use a strict CSP to ONLY allow your main domain to iframe it.
        Content-Security-Policy "frame-ancestors https://$DOMAIN"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
}
EOF
    sudo systemctl enable caddy
    sudo systemctl restart caddy
elif [ "$PROXY_CHOICE" = "2" ]; then
    echo "Installing Nginx..."
    sudo apt install -y nginx certbot python3-certbot-nginx
    
    sudo tee /etc/nginx/sites-available/lastwar > /dev/null <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN $COLLABORA_DOMAIN;
    
    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }
    location / {
        return 301 https://\$host\$request_uri;
    }
}
server {
    listen 443 ssl;
    server_name $DOMAIN;
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
server {
    listen 443 ssl;
    server_name $COLLABORA_DOMAIN;
    location / {
        proxy_pass http://localhost:9980;
        proxy_set_header Host \$host;
    }
    location ~ ^/cool/(.*)/ws$ {
        proxy_pass http://localhost:9980;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host \$host;
        proxy_read_timeout 36000s;
    }
    location ^~ /cool/adminws {
        proxy_pass http://localhost:9980;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host \$host;
        proxy_read_timeout 36000s;
    }
}
EOF
    sudo ln -sf /etc/nginx/sites-available/lastwar /etc/nginx/sites-enabled/
    sudo rm -f /etc/nginx/sites-enabled/default
    sudo systemctl enable nginx
    sudo systemctl restart nginx
fi

echo -e "${GREEN}[6/6] Pulling and starting Docker containers...${NC}"
sudo docker compose pull
sudo docker compose up -d

echo "Setting up daily backups..."
sudo tee /usr/local/bin/backup-lastwar.sh > /dev/null <<EOF
#!/bin/bash
BACKUP_DIR="/var/backups/lastwar"
DB_PATH="$APP_DIR/data/alliance.db"
DATE=\$(date +%Y%m%d_%H%M%S)

mkdir -p \$BACKUP_DIR
sudo sqlite3 \$DB_PATH ".backup '\$BACKUP_DIR/alliance_\$DATE.db'"
find \$BACKUP_DIR -name "alliance_*.db" -mtime +7 -delete
EOF
sudo chmod +x /usr/local/bin/backup-lastwar.sh
sudo mkdir -p /var/log/lastwar
(sudo crontab -l 2>/dev/null; echo "0 2 * * * /usr/local/bin/backup-lastwar.sh >> /var/log/lastwar/backup.log 2>&1") | sudo crontab -

echo -e "${GREEN}Installation Complete!${NC}"