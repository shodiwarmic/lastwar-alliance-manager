# Debian Server Deployment Guide

This guide covers the manual deployment of the Last War Alliance Manager. If you prefer an automated setup, use the included `install.sh` script.

## DNS Configuration (Pre-Requisite)

Before beginning the deployment, you **must** configure your domain's DNS. The document management system requires a dedicated subdomain to route WOPI WebSocket traffic correctly.

Create two A-Records pointing to your server's IP address:
1. **Main App:** `your-domain.com` → `your.server.ip.address`
2. **Collabora:** `collabora.your-domain.com` → `your.server.ip.address`

Wait for DNS propagation (5-60 minutes) before proceeding to the reverse proxy steps, or SSL certificate generation will fail.

---

## Quick Setup with Caddy (Recommended - Automatic HTTPS)

Caddy automatically handles Let's Encrypt certificates with zero configuration and natively supports WebSockets without complex routing rules.

### 1. Install Go on Debian

```bash
# Install Go 1.21 or higher
wget [https://go.dev/dl/go1.21.6.linux-amd64.tar.gz](https://go.dev/dl/go1.21.6.linux-amd64.tar.gz)
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
```

### 2. Install Build & OCR Dependencies

```bash
sudo apt update
sudo apt install -y gcc build-essential curl wget git ufw fail2ban sqlite3

# Install Tesseract OCR dependencies for image recognition
sudo apt install -y tesseract-ocr tesseract-ocr-all libtesseract-dev libleptonica-dev
```

### 3. Install Docker & Start Collabora

The document editing features require the Collabora CODE engine running in a local Docker container.

```bash
# Install Docker Engine
sudo apt install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL [https://download.docker.com/linux/debian/gpg](https://download.docker.com/linux/debian/gpg) -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] [https://download.docker.com/linux/debian](https://download.docker.com/linux/debian) $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt update
sudo apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Start the Collabora container
# NOTE: Replace 'your-domain.com' with your actual main application domain.
sudo docker run -t -d -p 9980:9980 \
    -e "domain=your-domain.com" \
    -e "aliasgroup1=[https://your-domain.com:443](https://your-domain.com:443)" \
    -e "extra_params=--o:ssl.enable=false --o:ssl.termination=true" \
    --restart always \
    --cap-add MKNOD \
    collabora/code
```

### 4. Deploy Application

```bash
# Create application directory
sudo mkdir -p /opt/lastwar
sudo chown $USER:$USER /opt/lastwar

# Upload your files to /opt/lastwar
# (use scp, rsync, or git clone)

cd /opt/lastwar

# Download Go modules and build the application
go mod download
go build -o alliance-manager main.go

# Create data directories
sudo mkdir -p /var/lib/lastwar/files
sudo chown -R $USER:$USER /var/lib/lastwar
```

### 5. Create Systemd Service

Create `/etc/systemd/system/lastwar.service`. Be sure to replace the domain placeholder values.

```ini
[Unit]
Description=Last War Alliance Manager
After=network.target

[Service]
Type=simple
User=lastwar
Group=lastwar
WorkingDirectory=/opt/lastwar
Environment="DATABASE_PATH=/var/lib/lastwar/alliance.db"
Environment="PORT=8080"
Environment="APP_DOMAIN=your-domain.com"
Environment="COLLABORA_DOMAIN=collabora.your-domain.com"
ExecStart=/opt/lastwar/alliance-manager
Restart=always
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/lastwar
CapabilityBoundingSet=
AmbientCapabilities=
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

[Install]
WantedBy=multi-user.target
```

### 6. Create Dedicated User

```bash
sudo useradd -r -s /bin/false -d /opt/lastwar lastwar
sudo chown -R lastwar:lastwar /opt/lastwar
sudo chown -R lastwar:lastwar /var/lib/lastwar
```



### 7. Install & Configure Caddy Reverse Proxy

```bash
# Install Caddy
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf '[https://dl.cloudsmith.io/public/caddy/stable/gpg.key](https://dl.cloudsmith.io/public/caddy/stable/gpg.key)' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf '[https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt](https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt)' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install caddy
```

Create `/etc/caddy/Caddyfile`:

```caddyfile
your-domain.com {
    reverse_proxy localhost:8080
    encode gzip
    
    # Security headers
    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
    
    # Logging
    log {
        output file /var/log/caddy/lastwar-access.log {
            roll_size 100mb
            roll_keep 5
        }
    }
}

# Collabora Document Server Subdomain
collabora.your-domain.com {
    encode gzip
    reverse_proxy localhost:9980
}
```

### 8. Configure Firewall

```bash
# Install and configure UFW
sudo apt install -y ufw

# Allow SSH (IMPORTANT - do this first!)
sudo ufw allow 22/tcp

# Allow HTTP and HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Enable firewall
sudo ufw enable
sudo ufw status
```

### 9. Start Services

```bash
# Start application
sudo systemctl daemon-reload
sudo systemctl enable lastwar
sudo systemctl start lastwar
sudo systemctl status lastwar

# Start Caddy (automatically gets Let's Encrypt certs for both domains)
sudo systemctl enable caddy
sudo systemctl restart caddy
sudo systemctl status caddy
```

---

## Alternative: Nginx with Certbot

If you prefer Nginx, follow steps 1-6 above, then proceed here:

### 1. Install Nginx and Certbot

```bash
sudo apt install -y nginx certbot python3-certbot-nginx
```

### 2. Configure Nginx

Create `/etc/nginx/sites-available/lastwar`:

```nginx
# Redirect HTTP to HTTPS for both domains
server {
    listen 80;
    listen [::]:80;
    server_name your-domain.com collabora.your-domain.com;
    
    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }
    
    location / {
        return 301 https://$host$request_uri;
    }
}

# HTTPS Server: Main Go Application
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name your-domain.com;
    
    # SSL config will be injected here by certbot
    
    # Security headers
    add_header X-Frame-Options "DENY" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    
    # Proxy to Go application
    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

# HTTPS Server: Collabora Document Server
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name collabora.your-domain.com;
    
    # SSL config will be injected here by certbot

    # Standard location
    location / {
        proxy_pass http://localhost:9980;
        proxy_set_header Host $host;
    }

    # WebSockets for Collabora Document Editing
    location ~ ^/cool/(.*)/ws$ {
        proxy_pass http://localhost:9980;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 36000s;
    }
    
    location ^~ /cool/adminws {
        proxy_pass http://localhost:9980;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "Upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 36000s;
    }
}
```

### 3. Enable Site and Get Certificates

```bash
# Enable site
sudo ln -sf /etc/nginx/sites-available/lastwar /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl enable nginx
sudo systemctl restart nginx

# Get Let's Encrypt certificates for BOTH domains
sudo certbot --nginx -d your-domain.com -d collabora.your-domain.com

# Test auto-renewal
sudo certbot renew --dry-run
```

### 4. Start Application Services

```bash
sudo systemctl start lastwar
```

---

## Additional Security Hardening

### 1. Keep System Updated

```bash
# Enable automatic security updates
sudo apt install -y unattended-upgrades
sudo dpkg-reconfigure -plow unattended-upgrades

# Manual updates
sudo apt update && sudo apt upgrade -y
```

### 2. Configure Fail2ban

```bash
# Create custom jail
sudo tee /etc/fail2ban/jail.local << 'EOF'
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
sudo systemctl start fail2ban
```

### 3. Secure SSH

Edit `/etc/ssh/sshd_config`:

```bash
# Disable root login
PermitRootLogin no

# Disable password authentication (use SSH keys)
PasswordAuthentication no
PubkeyAuthentication yes
```

Then restart SSH:
```bash
sudo systemctl restart sshd
```

### 4. Database Backups

```bash
# Create backup script
sudo tee /usr/local/bin/backup-lastwar.sh << 'EOF'
#!/bin/bash
BACKUP_DIR="/var/backups/lastwar"
DB_PATH="/var/lib/lastwar/alliance.db"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
sqlite3 $DB_PATH ".backup '$BACKUP_DIR/alliance_$DATE.db'"

# Keep only last 7 days
find $BACKUP_DIR -name "alliance_*.db" -mtime +7 -delete
EOF

sudo chmod +x /usr/local/bin/backup-lastwar.sh

# Add to crontab (daily at 2 AM)
echo "0 2 * * * /usr/local/bin/backup-lastwar.sh" | sudo crontab -
```

---

## Quick Troubleshooting

```bash
# Check if Go application is running
sudo systemctl status lastwar

# Check if Collabora container is running
sudo docker ps | grep collabora

# Check application logs
sudo journalctl -u lastwar -n 50

# Check if port 8080 and 9980 are listening
sudo ss -tlnp | grep -E '8080|9980'

# Restart everything
sudo systemctl restart lastwar
sudo docker restart $(sudo docker ps -q --filter ancestor=collabora/code)
sudo systemctl restart caddy  # or nginx
```

---

## Update Procedure

We strongly recommend using the included `update.sh` script, which automatically handles full application backups, database backups, dependency checks, and safe fallbacks if compilation fails.

```bash
cd /opt/lastwar

# If pulling from a Git repository:
sudo ./update.sh --git
```

### Manual Update Procedure (Fallback)

If you need to update manually without the script:

```bash
# Stop service
sudo systemctl stop lastwar

# Backup database and application
sudo cp /var/lib/lastwar/alliance.db /var/lib/lastwar/alliance.db.backup
sudo tar -czf /var/backups/lastwar/app_backup.tar.gz -C /opt/lastwar --exclude='.git' .

# Update code
cd /opt/lastwar
git pull

# Rebuild
export PATH=$PATH:/usr/local/go/bin
go mod download
go build -o alliance-manager .

# Restart service
sudo systemctl start lastwar
sudo systemctl status lastwar
```