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

The easiest way to deploy the application on a fresh Debian or Ubuntu server is using the included installation script. It will automatically install Docker, securely generate your environment variables and encryption keys, download the pre-built containers from the registry, and set up Caddy or Nginx with automatic HTTPS.

```bash
git clone [https://github.com/shodiwarmic/lastwar-alliance-manager.git](https://github.com/shodiwarmic/lastwar-alliance-manager.git) /opt/lastwar
cd /opt/lastwar
chmod +x install.sh
sudo ./install.sh
```

---

## 3. Manual Docker Deployment

If you prefer to set up the infrastructure yourself or are integrating this into an existing Docker environment, follow these steps.

### Step A: Install Docker
Ensure Docker and Docker Compose are installed on your system. 
[Official Docker Installation Guide](https://docs.docker.com/engine/install/)

### Step B: Prepare the Environment
```bash
# Clone the repository
git clone [https://github.com/shodiwarmic/lastwar-alliance-manager.git](https://github.com/shodiwarmic/lastwar-alliance-manager.git) /opt/lastwar
cd /opt/lastwar

# Create persistent storage directories
mkdir -p ./data ./uploads

# Configure environment variables
cp .env.example .env
nano .env
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

### Step C: Pull and Start the Stack
```bash
docker compose pull
docker compose build --no-cache
docker compose up -d
```
This will download the latest images, compile the Go binary, create a private internal bridge network, start the Go application (exposing port `8080`), and start the Collabora document server (exposing port `9980`).

---

## 4. Reverse Proxy Configuration

You must put the Docker containers behind a reverse proxy to handle SSL termination. 

### Option A: Caddy (Recommended)
Caddy automatically handles Let's Encrypt certificates and WebSockets natively.
Create or edit `/etc/caddy/Caddyfile`:

```caddyfile
app.yourdomain.com {
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

collabora.yourdomain.com {
    encode gzip
    reverse_proxy localhost:9980
    header {
        X-Content-Type-Options "nosniff"
        # Strict CSP to allow only your main app to iframe the document editor
        Content-Security-Policy "frame-ancestors [https://app.yourdomain.com](https://app.yourdomain.com)"
        X-XSS-Protection "1; mode=block"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        -Server
    }
}
```

### Option B: Nginx
If you prefer Nginx, ensure you have `certbot` and `python3-certbot-nginx` installed for SSL.
Create `/etc/nginx/sites-available/lastwar`:

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
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
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
    add_header Content-Security-Policy "frame-ancestors [https://app.yourdomain.com](https://app.yourdomain.com)" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    
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
*Run `sudo certbot --nginx -d app.yourdomain.com -d collabora.yourdomain.com` to apply SSL.*

---

## 5. Additional Security Hardening

### Configure Firewall (UFW)
Ensure only necessary ports are exposed to the internet. Docker handles its own internal routing.
```bash
sudo ufw allow 22/tcp  # SSH
sudo ufw allow 80/tcp  # HTTP
sudo ufw allow 443/tcp # HTTPS
sudo ufw enable
```

### Database Backups
Because the database is stored in a Docker volume mapped to your host machine (`./data`), you can back it up easily using standard host cron jobs.

```bash
# Example backup command to add to crontab
sqlite3 /opt/lastwar/data/alliance.db ".backup '/var/backups/lastwar/alliance_$(date +%Y%m%d).db'"
```

---

## 6. Update Procedure

We strongly recommend using the included `update.sh` script. It automatically pulls the latest code, clears legacy dependencies from your go.mod cache, safely downloads the newest pre-built images, and checks your proxy configurations for security compliance.

```bash
cd /opt/lastwar
sudo ./update.sh
```

If updating manually:
```bash
git pull
go mod tidy
docker compose build --no-cache
docker compose up -d
```