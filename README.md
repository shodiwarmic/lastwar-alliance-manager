# Last War: Survival - Alliance Manager

A comprehensive, self-hosted web application for managing your alliance in the online game Last War: Survival. Track members, manage train schedules, award achievements, host alliance documents, and generate communication messages, all deployed seamlessly via Docker.

## Features

### Core Management
- **Advanced Authentication**: Secure login/logout with rolling session management to keep active users logged in while expiring idle sessions.
- **Configurable Password Policies**: Admin-controlled password complexity requirements (minimum length, uppercase, lowercase, numbers, special characters).
- **Password Security Lifecycle**: Enforce password expiration dates, track password history to prevent reuse, and trigger forced password resets.
- **Customizable Login Banner**: Server-Side Rendered (SSR) login screen messaging configurable by Admins/R5s.
- **Role-Based Permissions**: Granular access levels for Admin, R5, R4, R3, R2, and R1 ranks.
- **User-Member Linking**: Users are securely linked to alliance members with role inheritance.
- **Dynamic CSV Import**: Upload roster CSVs with any column order. Automatically maps Username, Rank, Power, and Level. Ignores garbage columns and safely applies default ranks to missing data.
- **Advanced Player Stats**: Track optional fields including Player Profession and Troop Levels (dynamically validated against configurable HQ Level caps).
- **Squad Tracking**: Admin-toggleable tracking for Hero Squad Types and Squad Power utilizing historical tracking tables to track progression over time.

### 📁 Alliance Files & Document Management
Powered by the WOPI protocol and an integrated **Collabora Online (CODE)** container, the app provides a Google Drive-like experience natively.

- **Live Document Editing**: Full browser-based collaborative editing for spreadsheets (`.xlsx`, `.csv`), text documents (`.docx`), and presentations.
- **Native Image Hosting**: Fast, secure distribution of alliance cheat sheets, war infographics, and maps.
- **Docker-Bridged Security**: Document data flows over a private, internal Docker network (`lastwar-net`), completely bypassing external firewalls and NAT hairpinning limits.
- **Theme Synchronization**: The document editor dynamically reads your application's state, matching your Light or Dark mode preference automatically.
- **Ownership & Safekeeping**: Uploaders retain absolute permanent rights. Account deletion is blocked until an Admin transfers a member's files to prevent data loss.

### Train Schedule & Ranking System
- **Auto-Schedule**: Automatically assign conductors for the week based on performance rankings.
- **Configurable Point System**: Customize points for awards, recommendations, and penalties (e.g., recent conductor fatigue).
- **Smart Conductor Selection**: Automatically selects top 7 performers.
- **Message Generators**: Create formatted weekly schedules and daily reminders (with specific ST/UK times) ready to copy-paste into alliance chat.

### OCR Image Recognition (Tesseract)
- **Automated Data Extraction**: Upload game screenshots to automatically extract VS Points or Power updates using built-in Tesseract OCR.
- **Intelligent Preprocessing**: AI-powered region detection removes headers and UI buttons, enhancing contrast for perfect reads.
- **Fuzzy Member Matching**: Automatically matches OCR text to database members.
- See [IMAGE_RECOGNITION.md](IMAGE_RECOGNITION.md) for detailed technical documentation.

---

## Infrastructure & Deployment

The application utilizes a multi-container **Docker Compose** stack powered by pre-built images from the GitHub Container Registry. This means you **do not** need to install Go, Tesseract, GCC, or SQLite on your host machine, and your server never has to waste resources compiling code. 

### Prerequisites (Production)
- A Linux Server (Debian or Ubuntu recommended).
- **DNS Records**: You MUST have two domains pointing to your server's IP:
  1. Main App: `app.yourdomain.com`
  2. Document Server: `collabora.yourdomain.com`

### Quick Install (Debian/Ubuntu)
We provide an automated script that installs Docker, generates secure secrets, configures the Caddy reverse proxy with SSL, and pulls the pre-built containers.

```bash
git clone [https://github.com/yourusername/lastwar.git](https://github.com/yourusername/lastwar.git)
cd lastwar
chmod +x install.sh
sudo ./install.sh
```

### Manual Docker Deployment
If you prefer to deploy manually or are updating an existing environment:

1. Copy `.env.example` to `.env` and fill in your domains and a secure `SESSION_KEY`.
2. Pull the latest images and start the stack:
```bash
docker compose pull
docker compose up -d
```

See [DEPLOYMENT.md](DEPLOYMENT.md) for comprehensive production setup details.

---

## Environment Variables

The application relies on a `.env` file in the root directory:
- `DATABASE_PATH` - Path to SQLite database file (Default: `/app/data/alliance.db`)
- `STORAGE_PATH` - Path to uploaded files (Default: `/app/uploads`)
- `SESSION_KEY` - 64-character hex string for session/CSRF encryption.
- `PRODUCTION` - Set to `true` to enforce secure cookies.
- `HTTPS` - Set to `true` when behind an SSL proxy.
- `APP_DOMAIN` - Your main domain (e.g., `app.example.com`).
- `COLLABORA_DOMAIN` - Your document domain (e.g., `collabora.example.com`).
- `TRUSTED_ORIGINS` - Comma-separated list of trusted IPs/Domains for CSRF validation.

## Default Login Credentials
- **Username**: `admin`
- **Password**: `admin123`

⚠️ **Important**: The system will force you to change this immediately upon first login!

---

## Security Architecture

- **Isolated Internal Networking**: The Go application and the Collabora document server communicate exclusively over a private Docker bridge (`lastwar-net`), preventing external data exposure.
- **Strict CSRF Protection**: All mutating endpoints (POST/PUT/DELETE) are protected by robust Cross-Site Request Forgery tokens integrated natively into the JS fetch interceptors.
- **Content-Security-Policy**: The automated Caddy setup injects strict `frame-ancestors` headers, ensuring your Collabora instance can *only* be embedded within your specific Alliance Manager domain.
- **Password Hashing**: Passwords are exclusively hashed with bcrypt before storage, accompanied by strict server-side complexity enforcement.
- **WOPI JWT**: Document editing sessions are secured with short-lived JSON Web Tokens.
- **Volume Persistence**: Databases and uploads are stored in persistent Docker volumes, surviving container rebuilds while remaining inaccessible to the public web root.