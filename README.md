# Last War: Survival - Alliance Manager

A comprehensive, self-hosted web application for managing your alliance in the online game Last War: Survival. Track member growth, monitor VS Duel activity, host alliance documents, and share feedback, all deployed seamlessly via Docker.

## Features

### ⚙️ Core Management
- **Advanced Authentication**: Secure login/logout with rolling session management to keep active users logged in while expiring idle sessions.
- **Configurable Password Policies**: Admin-controlled password complexity requirements (minimum length, uppercase, lowercase, numbers, special characters).
- **Password Security Lifecycle**: Enforce password expiration dates, track password history to prevent reuse, and trigger forced password resets.
- **Customizable Login Banner**: Server-Side Rendered (SSR) login screen messaging configurable by Admins/R5s.
- **Role-Based Permissions**: Granular access levels governed by a dynamic, admin-controlled permissions matrix (e.g., toggling who can manage the roster, view analytics, or see anonymous feedback authors).
- **Categorized Alias Engine**: Assign personal nicknames or authoritative global aliases to commanders. The system utilizes a strict hierarchy (Exact -> Personal -> Global -> OCR) to resolve identities during imports. Background `ocr` aliases keep machine-read corrections hidden from standard user searches.
- **Self-Service Profiles**: Users securely linked to an in-game commander can update their own stats, HQ level, and squad power through a rule-enforced dashboard.
- **Smart CSV Ingestion**: Upload VS Points or roster CSVs with dynamic column mapping. Features a backend-driven "Preview & Confirm" modal allowing administrators to validate data, calculate missing values (e.g., deducing Saturday from Weekly Totals), and manually map unresolved names before committing.
- **Advanced Player Stats**: Track optional fields including Player Profession and Troop Levels (dynamically validated against configurable HQ Level caps).

### 🏠 Overview Dashboard
- **Customizable Landing Page**: The default landing page is a per-user dashboard surfacing key alliance data at a glance without navigating between pages. Cards can be reordered via drag-and-drop and toggled on or off; preferences are saved per account.
- **Alliance Health Card**: Active member count, total alliance power, and percentage of members currently eligible for train.
- **VS Performance Card**: Current week total points, average per member, percentage meeting the configured minimum, and top/bottom 3 contributors. The minimum threshold is configurable in Settings.
- **Schedule Card**: The next 3 upcoming events from the active alliance schedule.
- **Diplomacy Card**: Active allies with their agreement type tags.
- **Leader Flags Card** *(R4/R5 only)*: Members falling below the weekly VS minimum, sorted by total. Helps officers identify who needs follow-up without manually scanning the VS page.
- **Members page** is still accessible via its own nav link at `/members`.

### 📈 Analytics & Activity Dashboard
- **Commander Growth Tracking**: Instantly calculate and visualize 7-day and 30-day power deltas for every member to easily identify top grinders and stagnant accounts.
- **VS Duel Leaderboards**: Track daily alliance duel contributions with massive, interactive stacked bar charts to see exactly where members excel (e.g., Radar vs. Tech day).
- **Alliance Composition**: Premium `Chart.js` visualizations breaking down the alliance's Troop Tiers and Primary Squad focuses (Tank/Aircraft/Missile).
- **Historical Integrity**: Power and Squad Power are tracked chronologically, preventing data loss and enabling long-term growth analysis.
- **Audit / Activity Log**: A structured, chronological audit trail of all write operations across the app — member changes, recruits, allies, storm, OC, awards, files, imports, and more. Consecutive creates of the same entity type by the same user within 15 minutes are batched into a single entry. Update events include a field-level diff (e.g. `status: interested → pending`). Accessible to R4/R5 by default via a dedicated `/activity` page with user and limit filters. Sensitive events (user accounts, permissions, settings, credentials, invitations) are visible to admins only.

### 📢 Shoutouts & Feedback (Zero-Trust Feedback Engine)
- **Semi-Anonymous by Default**: To encourage honest feedback, authors are strictly anonymous. The Go backend scrubs identifying data before the payload ever reaches the client.
- **Targeted Visibility**: Authors can restrict their feedback to specific alliance ranks (e.g., R4 and above). The backend silently drops these records from the database query for unauthorized viewers.
- **RBAC Anonymity Override**: Alliance leaders can configure specific ranks to possess the `view_anonymous_authors` permission via the Admin Settings matrix, allowing authorized moderators to see the true author for accountability.
- **Creator Anonymity Bypass**: Authors can optionally toggle a "Make my author name public" checkbox, bypassing the anonymity filters to give public kudos.
- **Creator Management**: Authors retain full control to edit or delete their own active shoutouts, while authorized moderators can curate the board.
- **Auto-Expiring**: Shoutouts automatically expire after 7 days, keeping the feedback loop relevant to current events.

### 🌩️ Desert Storm Planner
- **Task Force Configuration**: Set up two Task Forces (A/B) with custom time slots for coordinated Storm events.
- **Member Registration**: Members self-register for Storm participation; leaders get a live view of sign-ups by TF.
- **Group & Building Management**: Organize registered members into groups, assign them to specific buildings, and track assignments in real time.
- **Permission-Gated Access**: Separate `view_storm` and `manage_storm` permissions let you control who can see vs. administer the planner.

### 🗓️ Schedule Builder
- **Dynamic Policy Engine**: Build reusable weekly schedules with configurable time overrides, week offsets, and custom events.
- **Delta Spec Support**: Define relative adjustments on top of a base schedule for recurring edge cases.
- **Infographic Export**: Export finished schedules as shareable infographics.
- **Permission-Gated Access**: Separate `view_schedule` and `manage_schedule` permissions.

### 🎖️ Officer Command
- **Responsibility Directory**: A living org chart of standing alliance functions, grouped by domain (e.g. Membership, Relations, War). Not a task manager — no completion states or due dates.
- **Category & Responsibility Management**: Admins can create, rename, and delete categories and responsibilities inline without leaving the page.
- **Assignee Tracking**: Assign one or more members to each responsibility; chips display name and rank. Members can be added via a searchable picker and removed individually.
- **Frequency Badges**: Each responsibility carries a Daily / Weekly / Seasonal frequency, displayed as colour-coded pill badges.
- **Client-Side Filtering**: Filter the directory by assigned leader or frequency without round-trips to the server.
- **Drag-to-Reorder**: Categories and responsibilities within a category can be reordered by drag-and-drop; order is persisted server-side.
- **Permission-Gated Access**: Separate `view_officer_command` (R1–R5 default) and `manage_officer_command` (R4–R5 default) permissions control who can view vs. administer the directory.

### 🎯 Recruiting
- **Former Members**: Instead of deleting members and losing their history, officers can archive them (rank `EX`). Archived members remain in all historical records — train logs still show their name, VS point history is preserved. Officers with `manage_members` can view former members on the Members page ("Former" filter chip) and on the Recruiting page with stats: last known power, total train runs conducted, and last VS week active.
- **Reactivate**: Former members can be restored to an active rank (R1–R5) from the Recruiting page, re-joining the roster immediately.
- **Prospect Tracking**: Officers can log potential recruits with fields for in-game name, server, current alliance, power, rank in their alliance, recruiter, first contact date, notes, and status (Interested / Pending / Declined).
- **Officer Notes**: Each active member now has an internal notes field visible only to officers with `manage_members`. Notes are displayed in the member edit modal and never shown to standard members.
- **Permission-Gated Access**: `manage_members` controls archive/reactivate and viewing former members. Separate `view_recruiting` and `manage_recruiting` permissions (R4–R5 default) gate the Recruiting page and prospect CRUD. Hard-deleting archived members is admin-only.

### ⚖️ Member Accountability
- **Tag System**: Each active member is automatically tagged as Reliable, Needs Improvement, or At Risk based on their current active strike count. The strike thresholds for each tag are configurable in Alliance Settings.
- **Hybrid Strike System**: VS performance is auto-flagged against the configurable weekly minimum (set in Alliance Settings). Officers review flagged members and add strikes with one click — duplicate VS strikes for the same week are blocked. Storm no-shows and train no-shows are manually logged by officers post-event; all other strikes can be added manually with a reason and optional reference date.
- **Train No-Show Tracking**: Officers can mark any train log entry as a no-show directly from the Train Tracker page. Doing so auto-creates an accountability strike; toggling it back removes the strike.
- **Storm Attendance Logging**: Officers log post-event storm attendance from the Accountability page — select the storm date, then mark each member as attended, no-show, or excused with an optional reason.
- **Member Profile**: Each member has a dedicated accountability profile showing their current tag and strike count, full strike history (with excuse/delete controls for officers), VS history for the last 8 weeks, storm attendance history, and train log.
- **Weekly Report**: A summary page showing top VS performers, members below the VS minimum, top power growth, and a breakdown of members by tag.
- **Dashboard Card**: An Accountability card on the dashboard surfaces the At Risk / Needs Improvement / Reliable counts and the three members with the most active strikes at a glance.
- **Permission-Gated Access**: `view_accountability` and `manage_accountability` default to R4/R5. Members cannot view their own accountability profile.

### 🚂 Train Tracker
- **Eligibility Rule Engine**: Officers create and save named eligibility rules using flexible OR-group / AND-condition logic to define who qualifies to conduct a train. Conditions can filter on member rank, current/previous week VS points, individual VS day columns, and days since last FREE or any train conducted.
- **Configurable Selection**: Each rule stores a selection method — Random, Greatest, or Least — applied to any tracked field (e.g. "prioritise members who have gone longest without conducting a FREE train").
- **Conductor Log**: Every train run is recorded with a game date (UTC-2), train type (FREE or PURCHASED), conductor, optional VIP slot (Special Guest or Guardian Defender), and notes. All members can view the full history with date-range filtering.
- **Soft Daily Limits**: Configurable per-type daily limits (default: 1 FREE, 2 PURCHASED). Exceeding the limit shows a warning — no hard block.
- **Permission-Gated Access**: Separate `view_train` (R1–R5 default) and `manage_train` (R4–R5 default) permissions control who can view history vs. manage logs and rules.

### 📁 Alliance Files & Document Management
Powered by the WOPI protocol and an integrated **Collabora Online (CODE)** container, the app provides a Google Drive-like experience natively.
- **Live Document Editing**: Full browser-based collaborative editing for spreadsheets (`.xlsx`, `.csv`), text documents (`.docx`), and presentations.
- **Native Image Hosting**: Fast, secure distribution of alliance cheat sheets, war infographics, and maps.
- **Docker-Bridged Security**: Document data flows over a private, internal Docker network (`lastwar-net`), completely bypassing external firewalls and NAT hairpinning limits.
- **Theme Synchronization**: The document editor dynamically reads your application's state, matching your Light or Dark mode preference automatically.

### 📸 Smart OCR Extraction (External Microservice)
To maintain a lightweight core application, heavy image processing and Optical Character Recognition (OCR) are offloaded to a dedicated, containerized Python microservice. 
**Repository:** [`shodiwarmic/lastwar-ocr-service`](https://github.com/shodiwarmic/lastwar-ocr-service)
- **Automated Data Extraction**: Drag and drop up to 100 game screenshots at once to automatically extract VS Points or Power updates using Google Cloud Vision Document AI.
- **Intelligent Pipeline**: The microservice automatically detects the screenshot type by analyzing colored UI tabs, groups them into buckets, and dynamically stitches them into vertical towers to bypass API limits and retain razor-sharp text.
- **Hybrid State Machine Parsing**: Overcomes vertical text-flow layout issues natively by intelligently pairing player names with valid scores while filtering out UI noise.
- **Validation UI & Machine Learning**: OCR results are held in a "Preview & Confirm" modal. Administrators can manually map unresolved scans to existing members and save the pairing as an `ocr` alias, teaching the Alias Engine to automatically correct that specific visual artifact in all future uploads.
- See [image_recognition.md](image_recognition.md) for detailed technical documentation.

---

## Infrastructure & Deployment

The application utilizes a multi-container **Docker Compose** stack powered by pre-built images from the GitHub Container Registry. This means you **do not** need to install Go, heavy C++ OCR libraries, or SQLite on your host machine, and your server never has to waste resources compiling code. 

### Prerequisites (Production)
- A Linux Server (Debian or Ubuntu recommended).
- **DNS Records**: You MUST have two domains pointing to your server's IP:
  1. Main App: `app.yourdomain.com`
  2. Document Server: `collabora.yourdomain.com`

### Quick Install (Debian/Ubuntu)
We provide an automated script that installs Docker, generates secure secrets, configures the Caddy reverse proxy with SSL, and pulls the pre-built containers.

```bash
git clone [https://github.com/shodiwarmic/lastwar-alliance-manager.git](https://github.com/shodiwarmic/lastwar-alliance-manager.git)
cd lastwar-alliance-manager
./install.sh
```

### Manual Docker Deployment
If you prefer to deploy manually or are updating an existing environment:

1. Copy `.env.example` to `.env` and fill in your domains, a secure `SESSION_KEY`, and a `CREDENTIAL_ENCRYPTION_KEY`.
2. Pull the latest images and start the stack:
```bash
docker compose pull
docker compose up -d
```

See [deployment.md](deployment.md) for comprehensive production setup details.

---

## Environment Variables

The application relies on a `.env` file in the root directory:
- `DATABASE_PATH` - Path to SQLite database file (Default: `/app/data/alliance.db`)
- `STORAGE_PATH` - Path to uploaded files (Default: `/app/uploads`)
- `SESSION_KEY` - 64-character hex string for session/CSRF encryption.
- `CREDENTIAL_ENCRYPTION_KEY` - 64-character hex string for AES-GCM encryption of external API credentials.
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

- **Encrypted Credentials**: External API credentials (like GCP Service Accounts) are symmetrically encrypted at rest using AES-GCM. The application employs strict memory hygiene, zeroing out sensitive plaintext buffers immediately after cryptographic operations or API transmissions to prevent memory scraping.
- **Isolated Internal Networking**: The Go application and the Collabora document server communicate exclusively over a private Docker bridge (`lastwar-net`), preventing external data exposure.
- **Strict CSRF Protection**: All mutating endpoints (POST/PUT/DELETE) are protected by robust Cross-Site Request Forgery tokens integrated natively into the JS fetch interceptors.
- **Content-Security-Policy**: The automated Caddy setup injects strict `frame-ancestors` headers, ensuring your Collabora instance can *only* be embedded within your specific Alliance Manager domain.
- **Password Hashing**: Passwords are exclusively hashed with bcrypt before storage, accompanied by strict server-side complexity enforcement.
- **WOPI JWT**: Document editing sessions are secured with short-lived JSON Web Tokens.
- **Volume Persistence**: Databases and uploads are stored in persistent Docker volumes, surviving container rebuilds while remaining inaccessible to the public web root.

### Enabling the OCR Microservice
To enable the Smart OCR Extraction features, you must deploy the [`lastwar-ocr-service`](https://github.com/shodiwarmic/lastwar-ocr-service) and configure your Go backend to communicate with it securely:

1. Generate a 32-byte hex string to serve as your server's cryptographic vault key:
```bash
openssl rand -hex 32
```
2. Set the output as the `CREDENTIAL_ENCRYPTION_KEY` in your `.env` file and start the Go server.
3. Log in as an Admin and navigate to the **Settings** dashboard.
4. Provide the deployed URL of your Python CV Worker.
5. Upload your Google Cloud Service Account JSON key (with Cloud Vision API access). The Go backend will encrypt this at rest and use it to securely invoke the private Cloud Run endpoint via OIDC tokens.