# Last War: Survival - Alliance Manager

A comprehensive Go web application for managing your alliance in the online game Last War: Survival. Track members, manage train schedules, award achievements, and generate communication messages.

## Features

### Core Management
- **Advanced Authentication**: Secure login/logout with rolling session management to keep active users logged in while expiring idle sessions.
- **Configurable Password Policies**: Admin-controlled password complexity requirements (minimum length, uppercase, lowercase, numbers, special characters).
- **Password Security Lifecycle**: Enforce password expiration dates, track password history to prevent reuse, and trigger forced password resets for specific users.
- **Customizable Login Banner**: Server-Side Rendered (SSR) login screen messaging configurable by Admins/R5s.
- **Role-Based Permissions**: Different access levels for Admin, R5, R4, and lower ranks.
- **User-Member Linking**: Users are linked to alliance members with role inheritance.
- **Member Management**: Add, edit, delete alliance members, with safe cascading deletions for linked user accounts and historical data.
- **Rank System**: Pre-configured with 5 ranks (R5, R4, R3, R2, R1).

### Train Schedule System
- **Weekly Schedule Management**: Organize and track train conductors and backups.
- **Auto-Schedule**: Automatically assign conductors for the week based on performance rankings.
- **Performance Tracking**: Track conductor scores and show-up history.
- **Weekly Message Generator**: Create formatted messages for alliance chat with schedules.
- **Daily Message Generator**: Generate daily reminders for conductors and backups with specific times (15:00 ST/17:00 UK for conductor, 16:30 ST/18:30 UK for backup).

### Awards & Recommendations
- **Weekly Awards**: Track 1st, 2nd, and 3rd place winners across multiple categories.
- **Recommendations**: Member recommendation system to boost rankings.
- **Performance Rankings**: Real-time leaderboard with detailed score breakdown.

### Ranking & Auto-Schedule System
- **Configurable Point System**: Customize points for awards, recommendations, and penalties.
- **Smart Conductor Selection**: Automatically selects top 7 performers as conductors.
- **Fair Distribution**: Penalties for recent conductors and above-average usage.
- **Rank Boosts**: Special bonuses for R4/R5 members and first-time conductors.
- **Backup System**: Smart backup assignment from R4/R5 members not in conductor pool.

### Communication Tools
- **Customizable Templates**: Configure weekly and daily message templates.
- **Train-Themed Messaging**: Fun, themed messages using train lingo ("ALL ABOARD", "Conductor", "Backup Engineer").
- **Placeholder System**: Dynamic message generation with member names, ranks, dates, and times.
- **Copy-to-Clipboard**: Easy copying of generated messages for in-game chat.

### Screenshot Upload with Image Recognition
- **OCR Processing**: Upload game screenshots for automatic data extraction using Tesseract OCR.
- **Intelligent Image Preprocessing**: AI-powered region detection and enhancement.
  - Automatically detects and crops data regions (removes headers, tabs, buttons).
  - Enhances contrast and applies scaling for better text recognition.
  - Filters out UI elements to focus only on relevant data.
- **Smart Parsing**: Advanced pattern matching for names and numeric values.
- **Fuzzy Member Matching**: Automatically matches OCR text to database members.
- **Manual Entry**: Alternative text-based input for manual data entry.
- **Power History Tracking**: Track member power progression over time.
- **Mobile-Friendly Interface**: Dedicated upload page optimized for mobile devices.

See [IMAGE_RECOGNITION.md](IMAGE_RECOGNITION.md) for detailed technical documentation on the image analysis system.

### Additional Features
- **Profile Management**: Users can securely change passwords with real-time, reactive UI feedback enforcing active policy rules.
- **Settings Page**: R5/Admin-only configuration for ranking systems, message templates, login banners, and strict password security rules.
- **Responsive UI**: Clean, modern interface that works seamlessly on desktop and mobile.
- **Advanced Filtering & Sorting**: Multi-select rank chips, eligibility toggles, and multi-criteria sorting (Name, Rank, Power).
- **SQLite Database**: Lightweight, file-based storage for easy deployment.

## Prerequisites

### Development
- **Go 1.21 or higher** - Download from https://golang.org/dl/
- **GCC compiler** (for CGO compilation - required for Tesseract OCR):
  - Windows: Install MinGW-w64 or TDM-GCC
  - Linux: `sudo apt-get install build-essential`
  - macOS: Install Xcode Command Line Tools
- **Tesseract OCR** (for image recognition features):
  - Windows: Download from https://github.com/UB-Mannheim/tesseract/wiki
  - Linux: `sudo apt-get install tesseract-ocr tesseract-ocr-all libtesseract-dev libleptonica-dev`
  - macOS: `brew install tesseract`

**Note**: CGO must be enabled for OCR features (`go env CGO_ENABLED` should return `1`). On Windows without MinGW/TDM-GCC, the application can compile but OCR features won't work. Deploy to Linux for full functionality.

### Production (Debian/Ubuntu Server)
See [DEPLOYMENT.md](DEPLOYMENT.md) for comprehensive production deployment guide with:
- Automated installation script
- Let's Encrypt SSL setup (Caddy or Nginx)
- Security hardening
- Systemd service configuration
- Firewall and fail2ban setup
- Automated backups

## Installation

### Development Setup

1. Navigate to the project directory
2. Download dependencies:
```bash
go mod download
```

### Production Deployment

**Quick Install (Debian/Ubuntu):**
```bash
chmod +x install.sh
./install.sh
```

See [DEPLOYMENT.md](DEPLOYMENT.md) for detailed manual setup instructions.

## Running the Application

### Development Mode

Build and run the server:
```bash
go run main.go
```

Or build an executable:
```bash
go build -o alliance-manager
./alliance-manager
```

The application will be available at `http://localhost:8080`

### Production Mode

```bash
# Set environment variables
export SESSION_KEY=$(openssl rand -hex 32)
export DATABASE_PATH=/var/lib/lastwar/alliance.db
export PRODUCTION=true
export HTTPS=true

# Build and run
go build -o alliance-manager main.go
./alliance-manager
```

Or use the systemd service (see [DEPLOYMENT.md](DEPLOYMENT.md)).

## Environment Variables

- `DATABASE_PATH` - Path to SQLite database file (default: `./alliance.db`)
- `SESSION_KEY` - 64-character hex string for session encryption (auto-generated if not set)
- `PRODUCTION` - Set to `true` for production mode (enables secure cookies)
- `HTTPS` - Set to `true` when using HTTPS (enables secure cookie flag)
- `PORT` - Server port (default: `8080`)

## Default Login Credentials

- **Username**: `admin`
- **Password**: `admin123`

⚠️ **Important**: Change the default password immediately after first login!

## Project Structure

```text
LastWar/
├── main.go             # Go server and API routes
├── go.mod              # Go module dependencies
├── go.sum              # Go module checksums
├── Dockerfile          # Docker container configuration
├── alliance.db         # SQLite database (created automatically)
├── install.sh          # Automated Debian installation script
├── update.sh           # Automated application update script
├── lastwar.service     # Systemd service configuration
├── Caddyfile           # Caddy reverse proxy configuration
├── .env.example        # Environment variables example
├── DEPLOYMENT.md       # Production deployment guide
├── QUICKSTART.md       # Quick setup instructions
├── IMAGE_RECOGNITION.md# Technical docs for OCR implementation
├── templates/          # Server-Side Rendered (SSR) HTML files
│   ├── layout.html     # Shared UI shell and navigation
│   ├── index.html      # Member management page
│   ├── login.html      # Login page (standalone)
│   ├── profile.html    # User profile & password management
│   ├── train.html      # Train schedule management
│   ├── awards.html     # Awards tracking
│   ├── recommendations.html # Recommendation system
│   ├── dyno.html       # Dyno recommendation system
│   ├── rankings.html   # Performance rankings
│   ├── storm.html      # Desert Storm assignment management
│   ├── vs.html         # VS Points tracking
│   ├── upload.html     # OCR Screenshot upload & processing
│   ├── admin.html      # Admin dashboard & user management
│   └── settings.html   # Configuration (R5/Admin only)
└── static/             # Client-side assets
    ├── styles.css      # Application styling
    ├── favicon.ico     # Site icon
    ├── global.js       # Shared utility functions
    ├── theme.js        # Light/Dark mode toggling
    ├── app.js          # Member management logic
    ├── profile.js      # Profile page logic
    ├── train.js        # Train schedule logic
    ├── awards.js       # Awards tracking logic
    ├── recommendations.js # Recommendations logic
    ├── dyno.js         # Dyno recommendations logic
    ├── rankings.js     # Rankings display logic
    ├── storm.js        # Desert Storm assignment logic
    ├── vs.js           # VS Points tracking logic
    ├── upload.js       # Upload and OCR processing logic
    ├── admin.js        # Admin user management logic
    └── settings.js     # Settings configuration logic
```

## Ranks

- **R5** - Highest rank - Can manage all members
- **R4** - Second highest rank - Can manage all members
- **R3** - Mid-level rank - View only
- **R2** - Lower rank - View only
- **R1** - Lowest rank - View only

## Permissions

- **Admin**: Full access to all features, R5/Admin-only settings (not linked to a member)
- **R5 Members**: Can manage members, create user accounts, update settings, manage all schedules, upload screenshots
- **R4 Members**: Can manage members and schedules, upload screenshots (cannot update settings or create users)
- **R3 Members**: Can upload screenshots and view all information (cannot modify members or schedules)
- **R2/R1 Members**: Can view all information but cannot modify or upload

### R5/Admin-Only Features
- Create user accounts for members
- Update ranking system configuration
- Modify message templates
- Configurable password complexity, history, and expiration rules
- Change all system settings

### Upload Features (R3+)
- Upload power ranking screenshots
- Upload VS Points screenshots  
- Manual data entry for power/VS points

## Technologies Used

- **Backend**: Go, Gorilla Mux
- **Database**: SQLite3 with go-sqlite3 driver
- **Frontend**: Vanilla HTML/CSS/JavaScript
- **Styling**: Modern gradient design with responsive layout

## API Endpoints

### Authentication
- `POST /api/login` - User login (detects forced resets/expirations)
- `POST /api/force-change-password` - Intercept endpoint for forced password updates
- `POST /api/logout` - User logout
- `GET /api/check-auth` - Check authentication status
- `POST /api/change-password` - Change user password

### Member Management (Protected)
- `GET /api/members` - Get all members
- `POST /api/members` - Create a new member (R4/R5 only)
- `PUT /api/members/{id}` - Update a member (R4/R5 only)
- `DELETE /api/members/{id}` - Delete a member (R4/R5 only)
- `POST /api/members/{id}/create-user` - Create user account for member (R5/Admin only)

### Train Schedule (Protected)
- `GET /api/train-schedules` - Get all schedules
- `POST /api/train-schedules` - Create schedule entry
- `PUT /api/train-schedules/{id}` - Update schedule
- `DELETE /api/train-schedules/{id}` - Delete schedule
- `POST /api/train-schedules/auto-schedule` - Auto-assign week's conductors
- `GET /api/train-schedules/weekly-message` - Generate weekly message
- `GET /api/train-schedules/daily-message` - Generate daily conductor message

### Awards (Protected)
- `GET /api/awards` - Get all awards
- `POST /api/awards` - Save awards for a week

### Recommendations (Protected)
- `GET /api/recommendations` - Get all recommendations
- `POST /api/recommendations` - Add recommendation
- `DELETE /api/recommendations/{id}` - Remove recommendation

### Rankings (Protected)
- `GET /api/rankings` - Get member performance rankings

### Settings (R5/Admin Only)
- `GET /api/settings` - Get current settings
- `PUT /api/settings` - Update settings

## Notes

- The database file `alliance.db` will be created automatically on first run.
- Make sure port 8080 is available (or set PORT environment variable).
- All data is stored locally in the SQLite database.
- Default admin user is created automatically on first run.
- Session cookies are used for authentication with a 7-day rolling expiration to keep active users seamlessly logged in.
- Passwords are hashed using bcrypt. The application actively tracks password history to prevent reuse and enforces admin-defined complexity requirements.
- User creation generates secure random 10-character alphanumeric passwords.
- Auto-schedule uses a sophisticated ranking algorithm to ensure fair distribution.
- Message templates are fully customizable in the Settings page.
- Daily messages include specific times: 15:00 ST (17:00 UK) for conductor, 16:30 ST (18:30 UK) for backup.
- The application can be containerized using the provided Dockerfile.
- Set DATABASE_PATH environment variable for custom database location (useful for Docker volumes).

## How Auto-Schedule Works

The auto-schedule system calculates scores for each member based on:

1. **Award Points**: Points from last week's 1st/2nd/3rd place awards
2. **Recommendation Points**: Points per active recommendation
3. **R4/R5 Rank Boost**: Bonus points for R4 and R5 members
4. **First-Time Conductor Boost**: Extra points for members who've never been conductor
5. **Recent Conductor Penalty**: Reduced points if they were conductor recently
6. **Above Average Penalty**: Penalty for members who've been conductor more than average

The top 7 members are selected as conductors for the week. Backups are selected from R4/R5 members who are not conductors, with each backup used only once per week.

## Message Templates

### Weekly Message Placeholders
- `{WEEK}` - Week start date
- `{SCHEDULES}` - Daily conductor/backup list
- `{NEXT_3}` - Next 3 top-ranked candidates

### Daily Message Placeholders
- `{DATE}` - Formatted date (e.g., Monday, Jan 2, 2006)
- `{CONDUCTOR_NAME}` - Name of the conductor
- `{CONDUCTOR_RANK}` - Rank of the conductor
- `{BACKUP_NAME}` - Name of the backup
- `{BACKUP_RANK}` - Rank of the backup

## Security

- Passwords are exclusively hashed with bcrypt before storage.
- Strict server-side and real-time client-side password complexity enforcement.
- Built-in password history tracking and configurable expiration intervals.
- Rolling session expiration keeps active users logged in while automatically dropping idle sessions.
- Role-based access control for all sensitive operations.
- SQL injection prevention through parameterized queries.
- R5/Admin-only restrictions on critical system and security settings.