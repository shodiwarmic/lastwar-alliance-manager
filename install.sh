#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Last War Alliance Manager - Debian/Ubuntu Installation Script${NC}"
echo "========================================================"
echo ""

# Check if running as root
if [[ $EUID -eq 0 ]]; then
   echo -e "${RED}This script should NOT be run as root${NC}"
   echo "Please run as a regular user with sudo privileges"
   exit 1
fi

command -v sudo >/dev/null 2>&1 || { echo -e "${RED}sudo is required but not installed${NC}"; exit 1; }

# Variables
APP_NAME="lastwar"
APP_USER="lastwar"
APP_DIR="/opt/lastwar"
DATA_DIR="/var/lib/lastwar"
LOG_DIR="/var/log/lastwar"

# Detect OS for package repositories
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

echo -e "${GREEN}[1/10] Updating system packages...${NC}"
sudo apt update
sudo apt upgrade -y

echo -e "${GREEN}[2/10] Installing build dependencies...${NC}"
sudo apt install -y gcc build-essential curl wget git ufw fail2ban sqlite3

echo -e "${GREEN}[2a/10] Installing OCR dependencies...${NC}"
sudo apt install -y tesseract-ocr tesseract-ocr-all libtesseract-dev libleptonica-dev

echo -e "${GREEN}[3/10] Installing Go...${NC}"
if ! command -v go &> /dev/null; then
    GO_VERSION="1.21.6"
    wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
    rm go${GO_VERSION}.linux-amd64.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee -a /etc/profile
    export PATH=$PATH:/usr/local/go/bin
fi

echo -e "${GREEN}[3b/10] Installing Docker & Collabora...${NC}"
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

echo "Starting Collabora Document Server container..."
sudo docker run -t -d -p 9980:9980 \
    -e "domain=$DOMAIN" \
    -e "aliasgroup1=https://$DOMAIN:443" \
    -e "extra_params=--o:ssl.enable=false --o:ssl.termination=true" \
    --restart always \
    --cap-add MKNOD \
    collabora/code

echo -e "${GREEN}[4/10] Creating application user...${NC}"
if ! id "$APP_USER" &>/dev/null; then
    sudo useradd -r -s /bin/false -d $APP_DIR $APP_USER
fi

echo -e "${GREEN}[5/10] Creating application directories...${NC}"
sudo mkdir -p $APP_DIR
sudo mkdir -p $DATA_DIR/files
sudo mkdir -p $LOG_DIR
sudo chown -R $APP_USER:$APP_USER $APP_DIR
sudo chown -R $APP_USER:$APP_USER $DATA_DIR
sudo chown -R $APP_USER:$APP_USER $LOG_DIR

echo -e "${GREEN}[6/10] Building application...${NC}"
cd "$(dirname "$0")"
go build -o alliance-manager main.go
sudo cp alliance-manager $APP_DIR/
sudo cp -r static $APP_DIR/
sudo cp -r templates $APP_DIR/
sudo chown -R $APP_USER:$APP_USER $APP_DIR

echo -e "${GREEN}[7/10] Generating secure session key...${NC}"
SESSION_KEY=$(openssl rand -hex 32)

sudo tee $APP_DIR/.env > /dev/null <<EOF
DATABASE_PATH=$DATA_DIR/alliance.db
SESSION_KEY=$SESSION_KEY
PRODUCTION=true
HTTPS=true
PORT=8080
APP_DOMAIN=$DOMAIN
COLLABORA_DOMAIN=$COLLABORA_DOMAIN
EOF
sudo chown $APP_USER:$APP_USER $APP_DIR/.env
sudo chmod 600 $APP_DIR/.env

echo -e "${GREEN}[8/10] Installing systemd service...${NC}"
sudo cp lastwar.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable $APP_NAME

echo -e "${GREEN}[9/10] Configuring firewall...${NC}"
sudo ufw --force enable
sudo ufw allow 22/tcp comment 'SSH'
sudo ufw allow 80/tcp comment 'HTTP'
sudo ufw allow 443/tcp comment 'HTTPS'

echo -e "${GREEN}[10/10] Installing reverse proxy...${NC}"
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

echo "Configuring fail2ban..."
sudo tee /etc/fail2ban/jail.local > /dev/null <<EOF
[DEFAULT]
bantime = 3600
findtime = 600
maxretry = 5

[sshd]
enabled = true
port = ssh
logpath = /var/log/auth.log
EOF
sudo systemctl enable fail2ban
sudo systemctl restart fail2ban

echo "Setting up daily backups..."
sudo tee /usr/local/bin/backup-lastwar.sh > /dev/null <<'EOF'
#!/bin/bash
BACKUP_DIR="/var/backups/lastwar"
DB_PATH="/var/lib/lastwar/alliance.db"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
sqlite3 $DB_PATH ".backup '$BACKUP_DIR/alliance_$DATE.db'"
find $BACKUP_DIR -name "alliance_*.db" -mtime +7 -delete
EOF
sudo chmod +x /usr/local/bin/backup-lastwar.sh
(sudo crontab -l 2>/dev/null; echo "0 2 * * * /usr/local/bin/backup-lastwar.sh >> /var/log/lastwar/backup.log 2>&1") | sudo crontab -

sudo systemctl start $APP_NAME
echo -e "${GREEN}Installation Complete!${NC}"