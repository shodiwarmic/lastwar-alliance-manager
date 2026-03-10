#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}Last War Alliance Manager - Update Script${NC}"
echo "=========================================="
echo ""

# Check if running as root
if [[ $EUID -eq 0 ]]; then
   echo -e "${RED}This script should NOT be run as root${NC}"
   echo "Please run as a regular user with sudo privileges"
   exit 1
fi

# Variables
APP_NAME="lastwar"
APP_USER="lastwar"
APP_DIR="/opt/lastwar"
DATA_DIR="/var/lib/lastwar"
BACKUP_DIR="/var/backups/lastwar"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Check if application is installed
if [ ! -d "$APP_DIR" ]; then
    echo -e "${RED}Application directory not found: $APP_DIR${NC}"
    echo "Please run install.sh first"
    exit 1
fi

# Determine update method
UPDATE_METHOD=""
if [ -d "$APP_DIR/.git" ]; then
    UPDATE_METHOD="git"
    echo -e "${GREEN}Git repository detected - using git pull${NC}"
elif [ "$1" = "--git" ]; then
    UPDATE_METHOD="git"
    echo -e "${GREEN}Git mode requested${NC}"
else
    UPDATE_METHOD="scp"
    echo -e "${YELLOW}Using SCP mode (upload files manually before running this script)${NC}"
    echo -e "${YELLOW}To use git in the future, run: ${NC}./update.sh --setup-git <repository-url>"
fi

# Setup git repository if requested
if [ "$1" = "--setup-git" ]; then
    if [ -z "$2" ]; then
        echo -e "${RED}Please provide repository URL${NC}"
        echo "Usage: $0 --setup-git <repository-url>"
        exit 1
    fi
    
    REPO_URL="$2"
    echo -e "${GREEN}Setting up git repository...${NC}"
    
    # Backup current installation
    echo "Backing up current installation..."
    sudo mkdir -p $BACKUP_DIR
    sudo tar -czf $BACKUP_DIR/app_before_git_$TIMESTAMP.tar.gz -C $APP_DIR --exclude='.git' .
    
    # Initialize git
    cd $APP_DIR
    sudo -u $APP_USER git init
    sudo -u $APP_USER git remote add origin $REPO_URL
    sudo -u $APP_USER git fetch
    sudo -u $APP_USER git reset --hard origin/main || sudo -u $APP_USER git reset --hard origin/master
    
    echo -e "${GREEN}Git repository setup complete!${NC}"
    echo "You can now run: $0"
    exit 0
fi

echo ""
echo -e "${YELLOW}[1/8] Creating backup...${NC}"
sudo mkdir -p $BACKUP_DIR
# Backup the entire app directory (excluding .git) so source code and binaries stay synced
sudo tar -czf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C $APP_DIR --exclude='.git' .
sudo sqlite3 $DATA_DIR/alliance.db ".backup '$BACKUP_DIR/db_$TIMESTAMP.db'"
echo -e "${GREEN}Backup created:${NC}"
echo "  - $BACKUP_DIR/app_$TIMESTAMP.tar.gz"
echo "  - $BACKUP_DIR/db_$TIMESTAMP.db"

echo ""
echo -e "${YELLOW}[2/8] Stopping application...${NC}"
sudo systemctl stop $APP_NAME
echo "Application stopped"

# Store current working directory
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo ""
echo -e "${YELLOW}[3/8] Updating system dependencies...${NC}"
sudo apt update
# Added sqlite3 to ensure the database backup command always works
sudo apt install -y sqlite3 tesseract-ocr tesseract-ocr-all libtesseract-dev libleptonica-dev
echo "System and OCR dependencies updated"

if [ "$UPDATE_METHOD" = "git" ]; then
    echo ""
    echo -e "${YELLOW}[4/8] Pulling latest changes from git...${NC}"
    cd $APP_DIR
    
    # Stash any local changes (like .env)
    sudo -u $APP_USER git stash push -m "Auto-stash before update $TIMESTAMP"
    
    # Pull latest changes
    sudo -u $APP_USER git pull origin main || sudo -u $APP_USER git pull origin master
    
    # Restore stashed changes if any
    if sudo -u $APP_USER git stash list | grep -q "Auto-stash"; then
        sudo -u $APP_USER git stash pop || echo -e "${YELLOW}Note: Some stashed changes could not be automatically applied${NC}"
    fi
    
    echo -e "${GREEN}Git pull completed${NC}"
    
else
    echo ""
    echo -e "${YELLOW}[4/8] Copying new files...${NC}"
    
    # Check if we're running from the source directory
    if [ ! -f "$SCRIPT_DIR/main.go" ]; then
        echo -e "${RED}Error: main.go not found in $SCRIPT_DIR${NC}"
        echo "Please run this script from the project directory or upload files first"
        echo ""
        echo -e "${YELLOW}To upload files from local machine:${NC}"
        echo "  scp -r *.go *.html static/ templates/ go.mod user@server:/tmp/lastwar-update/"
        echo "  ssh user@server"
        echo "  cd /tmp/lastwar-update && sudo ./update.sh"
        exit 1
    fi
    
    # Copy all Go source files
    sudo cp "$SCRIPT_DIR/"*.go $APP_DIR/
    sudo cp "$SCRIPT_DIR/go.mod" $APP_DIR/ 2>/dev/null || true
    sudo cp "$SCRIPT_DIR/go.sum" $APP_DIR/ 2>/dev/null || true
    
    # Copy any root HTML files (like index.html)
    sudo cp "$SCRIPT_DIR/"*.html $APP_DIR/ 2>/dev/null || true

    # Copy static assets (CSS, JS, Images)
    if [ -d "$SCRIPT_DIR/static" ]; then
        sudo mkdir -p $APP_DIR/static
        sudo cp -r "$SCRIPT_DIR/static/"* $APP_DIR/static/
    fi

    # Copy templates directory (if you use one)
    if [ -d "$SCRIPT_DIR/templates" ]; then
        sudo mkdir -p $APP_DIR/templates
        sudo cp -r "$SCRIPT_DIR/templates/"* $APP_DIR/templates/
    fi
    
    # Preserve .env file
    if [ -f $APP_DIR/.env ]; then
        echo "Preserving environment configuration"
    fi
    
    echo -e "${GREEN}Files copied${NC}"
fi

echo ""
echo -e "${YELLOW}[5/8] Checking Go installation...${NC}"
# Use explicit path to avoid sudo stripping the environment variables
export PATH=$PATH:/usr/local/go/bin
if ! command -v go &> /dev/null; then
    echo -e "${RED}Go is not installed or not in PATH!${NC}"
    exit 1
fi
echo "Go version: $(go version)"

echo ""
echo -e "${YELLOW}[6/8] Building application...${NC}"
cd $APP_DIR

# Download dependencies if needed using explicit path
if [ -f "go.mod" ]; then
    sudo -u $APP_USER env PATH=$PATH:/usr/local/go/bin go mod download
fi

# Build using '.' to capture all .go files, with explicit path
sudo -u $APP_USER env PATH=$PATH:/usr/local/go/bin go build -o alliance-manager .

if [ ! -f "alliance-manager" ]; then
    echo -e "${RED}Build failed!${NC}"
    echo ""
    echo -e "${YELLOW}Rolling back...${NC}"
    sudo tar -xzf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C $APP_DIR
    sudo systemctl start $APP_NAME
    echo -e "${RED}Rollback complete. Old version restored.${NC}"
    exit 1
fi

sudo chown $APP_USER:$APP_USER alliance-manager
echo -e "${GREEN}Build successful${NC}"

echo ""
echo -e "${YELLOW}[7/8] Setting permissions...${NC}"
sudo chown -R $APP_USER:$APP_USER $APP_DIR/alliance-manager
sudo chown -R $APP_USER:$APP_USER $APP_DIR/static 2>/dev/null || true
sudo chown -R $APP_USER:$APP_USER $APP_DIR/templates 2>/dev/null || true
sudo chmod +x $APP_DIR/alliance-manager

echo ""
echo -e "${YELLOW}[8/8] Starting application...${NC}"
sudo systemctl start $APP_NAME
sleep 2

echo ""
echo -e "${YELLOW}[9/9] Verifying deployment...${NC}"
if sudo systemctl is-active --quiet $APP_NAME; then
    echo -e "${GREEN}✓ Application is running${NC}"
    sudo systemctl status $APP_NAME --no-pager -l | head -n 20
else
    echo -e "${RED}✗ Application failed to start!${NC}"
    echo ""
    echo -e "${YELLOW}Checking logs:${NC}"
    sudo journalctl -u $APP_NAME -n 50 --no-pager
    echo ""
    echo -e "${YELLOW}Rolling back...${NC}"
    sudo systemctl stop $APP_NAME
    sudo tar -xzf $BACKUP_DIR/app_$TIMESTAMP.tar.gz -C $APP_DIR
    sudo systemctl start $APP_NAME
    echo -e "${RED}Rollback complete. Old version restored.${NC}"
    exit 1
fi

# Cleanup old backups (keep last 10)
echo ""
echo -e "${YELLOW}Cleaning old backups...${NC}"
cd $BACKUP_DIR
ls -t app_*.tar.gz 2>/dev/null | tail -n +11 | xargs -r sudo rm
ls -t db_*.db 2>/dev/null | tail -n +11 | xargs -r sudo rm
echo "Kept 10 most recent backups"

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Update Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "Build timestamp: ${YELLOW}$TIMESTAMP${NC}"
echo -e "Backup location: ${YELLOW}$BACKUP_DIR/${NC}"
echo ""
echo -e "${YELLOW}Useful commands:${NC}"
echo "  sudo systemctl status lastwar    - Check service status"
echo "  sudo journalctl -u lastwar -f    - View live logs"
echo "  sudo systemctl restart lastwar   - Restart service"
echo ""

if [ "$UPDATE_METHOD" = "scp" ]; then
    echo -e "${YELLOW}To switch to git-based updates:${NC}"
    echo "  $0 --setup-git <repository-url>"
    echo ""
fi