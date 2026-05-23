# Last War: Survival - Alliance Manager

A comprehensive, self-hosted web application for managing your alliance in the online game Last War: Survival. Track member growth, monitor VS Duel activity, host alliance documents, and share feedback, all deployed seamlessly via Docker.

## Features

### ⚙️ Core Management
- **Advanced Authentication**: Secure login/logout with rolling session management to keep active users logged in while expiring idle sessions.
- **Configurable Password Policies**: Admin-controlled password complexity requirements (minimum length, uppercase, lowercase, numbers, special characters).
- **Password Security Lifecycle**: Enforce password expiration dates, track password history to prevent reuse, and trigger forced password resets.
- **Customizable Login Banner**: Server-Side Rendered (SSR) login screen messaging configurable by Admins/R5s.
- **Role-Based Permissions**: Granular access levels governed by a dynamic, admin-controlled permissions matrix (e.g., toggling who can manage the roster, view analytics, or see anonymous feedback authors).
- **Categorized Alias Engine**: Assign personal nicknames or authoritative global aliases to commanders. The system utilizes a strict hierarchy (Exact -> Personal -> Global -> OCR) to resolve identities during imports. Background `ocr` aliases keep machine-read corrections hidden from standard user searches. The alias modal on the Members page lets officers browse, add, and delete aliases per member, with filter tabs (All / Global / Personal / OCR) for pruning stale auto-generated OCR aliases.
- **Invite-Link Onboarding**: Officers with `manage_members` can generate a single-use, 48-hour invite link directly from the Members page. Following the link takes a new user to a self-registration page pre-linked to their in-game commander — no admin password-setting required.
- **Self-Service Profiles**: Users securely linked to an in-game commander can update their own stats, HQ level, and squad power through a rule-enforced dashboard.
- **Smart CSV Ingestion**: Upload VS Points or roster CSVs with dynamic column mapping. Features a backend-driven "Preview & Confirm" modal allowing administrators to validate data, calculate missing values (e.g., deducing Saturday from Weekly Totals), and manually map unresolved names before committing.
- **Advanced Player Stats**: Track optional fields including Player Profession, Squad Type, Squad Power, Total Hero Power, Lifetime Troop Kills, and Troop Levels (dynamically validated against configurable HQ Level caps). All stat badges on member cards adapt to light and dark mode. Members and officers with `manage_members` can record kills directly from the edit modal; self-linked users can update their kill count from My Profile.
- **Alliance Identity**: Configure a custom Alliance Name and Tag in Settings. The name replaces the generic "Alliance" label in the sidebar brand and mobile header; the tag (e.g. `[WARMC]`) appears in the subtitle beside "Alliance Manager". Both fields are optional — leave blank to keep the defaults.
- **Theme Switcher**: Each user can independently set the app theme to **Light**, **Dark**, or **Auto** (follows the OS preference). The selection persists across sessions via `localStorage`. Accessible from the gear icon in the sidebar user row or the More sheet on mobile.
- **Unified Navigation Theme**: The Admin nav item uses a purple tint (token-based, dark-mode safe) instead of the primary gradient, reducing visual competition with the active page indicator.
- **Sidebar Navigation**: On desktop (≥769 px) a persistent sidebar replaces the old top bar, providing always-visible navigation without a hamburger. On mobile a fixed bottom tab bar gives one-tap access to the five most-used pages; a "More" sheet slides up for the full nav.
- **Rank Badges**: R4–R1 rank badges use semantic color tokens (danger/info/success/muted) that adapt to light and dark mode. R5 retains its gold "prestige" color.
- **Consistent Member Badges**: HQ level, Troop tier, Profession, and Squad type badges on member cards use the design system's semantic color tokens and adapt correctly to light and dark mode.
- **Filter Chips**: The Members page rank/profession/troop/squad filter chips are compact pill-shaped buttons with a clear active state.
- **Members Search & Sort**: Live search on the Members page uses [Fuse.js](https://www.fusejs.io/) fuzzy matching so partial or misspelled commander names still surface the right result. Results can be sorted by name, HQ level, power, squad power, or join order, with ascending/descending toggle.

### 🏠 Overview Dashboard
- **Customizable Landing Page**: The default landing page is a per-user dashboard surfacing key alliance data at a glance without navigating between pages. Cards can be reordered via drag-and-drop and toggled on or off; preferences are saved per account.
- **Alliance Health Card**: Active member count, total alliance power, and percentage of members currently eligible for train.
- **VS Performance Card**: Current week total points, average per member, percentage meeting the configured minimum, and top/bottom 3 contributors. The minimum threshold is configurable in Settings.
- **Schedule Card**: The next 3 upcoming events from the active alliance schedule.
- **Diplomacy Card**: Active allies with their agreement type tags.
- **Leader Flags Card** *(R4/R5 only)*: Members falling below the weekly VS minimum, sorted by total. Helps officers identify who needs follow-up without manually scanning the VS page.
- **Members page** is still accessible via its own nav link at `/members`.

### 📈 Analytics & Activity Dashboard
- **Commander Growth Tracking**: Instantly calculate and visualize 7-day and 30-day overall power deltas for every member to easily identify top grinders and stagnant accounts.
- **Total Hero Power Tracking**: Track each member's Total Hero Power alongside overall power, with full chronological history and 7-day / 30-day growth deltas shown on the Tracking page.
- **Troop Kill Tracking**: Track each member's lifetime troop kill count with a full snapshot history. The Tracking page's dedicated Kills tab shows current kills, 7-day delta, and 30-day delta for every member who has a recorded entry. Kill counts can be entered manually from the member edit modal or My Profile, and are imported automatically when a Strength — Kills screenshot is processed through the OCR upload.
- **VS Duel Leaderboards**: Track daily alliance duel contributions with massive, interactive stacked bar charts to see exactly where members excel (e.g., Radar vs. Tech day).
- **Alliance Composition**: Premium `Chart.js` visualizations breaking down the alliance's Troop Tiers and Primary Squad focuses (Tank/Aircraft/Missile).
- **Theme-Aware Charts**: Chart.js color defaults are read from the active CSS theme at initialization time, so axis labels and tooltips match the current light/dark mode rather than being hardcoded to a single palette.
- **Historical Integrity**: Overall Power, Squad Power, and Total Hero Power are all tracked chronologically, preventing data loss and enabling long-term growth analysis.
- **Audit / Activity Log**: A structured, chronological audit trail of all write operations across the app — member changes, recruits, allies, storm, OC, awards, files, imports, and more. Consecutive creates of the same entity type by the same user within 15 minutes are batched into a single entry. Update events include a field-level diff (e.g. `status: interested → pending`). Accessible to R4/R5 by default via a dedicated `/activity` page with user and limit filters. Sensitive events (user accounts, permissions, settings, credentials, invitations) are visible to admins only.

### 📢 Shoutouts & Feedback (Zero-Trust Feedback Engine)
- **Semi-Anonymous by Default**: To encourage honest feedback, authors are strictly anonymous. The Go backend scrubs identifying data before the payload ever reaches the client.
- **Targeted Visibility**: Authors can restrict their feedback to specific alliance ranks (e.g., R4 and above). The backend silently drops these records from the database query for unauthorized viewers.
- **RBAC Anonymity Override**: Alliance leaders can configure specific ranks to possess the `view_anonymous_authors` permission via the Admin Settings matrix, allowing authorized moderators to see the true author for accountability.
- **Creator Anonymity Bypass**: Authors can optionally toggle a "Make my author name public" checkbox, bypassing the anonymity filters to give public kudos.
- **Creator Management**: Authors retain full control to edit or delete their own active shoutouts, while authorized moderators can curate the board.
- **Auto-Expiring**: Shoutouts automatically expire after 7 days, keeping the feedback loop relevant to current events.

### 🤝 Allies & Diplomacy
- **Ally Directory**: Track all current and former allied alliances on a dedicated `/allies` page. Each entry stores the ally name, agreement type tags, and active/inactive status. Inactive allies are hidden by default and can be surfaced via toggle.
- **Agreement Type Registry**: Manage a custom set of agreement types (e.g. NAP, Mutual Aid, Coalition) used to tag each relationship. Types can be created, renamed, and deleted from the Agreement Types tab (visible to `manage_allies`).
- **Dashboard Integration**: The Overview Dashboard's Diplomacy Card surfaces active allies and their tags at a glance without navigating to the full page.
- **Permission-Gated Access**: Separate `view_allies` (R4/R5 default) and `manage_allies` (R5 default) permissions control read vs. write access.

### 🌩️ Desert Storm Planner
- **Task Force Configuration**: Set up two Task Forces (A/B) with custom time slots for coordinated Storm events.
- **Member Registration**: Members self-register for Storm participation; leaders get a live view of sign-ups by TF.
- **Group & Building Management**: Organize registered members into groups, assign them to specific buildings, and track assignments in real time.
- **Battle Mail Integration**: The Battle Mail tab fetches the "DS Battle Strategy Mail" template from the Comms hub, pre-fills task force, battle time, and group assignments automatically, then copies straight to clipboard. Edit the template on the Comms page and Storm picks it up immediately; if the template is deleted Storm falls back to a built-in mail gracefully.
- **Permission-Gated Access**: Separate `view_storm` and `manage_storm` permissions let you control who can see vs. administer the planner.

### 🗓️ Alliance Schedule
- **Calendar-Based Events**: Schedule Marshal's Guard, Zombie Siege, and custom events on specific dates with exact server times. No more repeating templates — every event lives on a real calendar date.
- **Event Types**: A managed registry of event types. MG and ZS are built-in system types; officers can add custom types (SVS, etc.) with a name, short name, and icon.
- **All-Day Events**: Mark any event as all-day when no specific time applies; all-day events sort to the top of each day column.
- **Smart Validation**: MG events are blocked from starting at or after 22:00 ST (hard game rule). ZS events enforce the 71.5-hour cooldown — the UI shows the next eligible time if the cooldown hasn't elapsed.
- **Level Tracking**: MG and ZS events store a concrete level at creation time (defaults to the configured baseline). Updating the baseline never retroactively changes past events.
- **Event Generation**: Bulk-generate MG events (every-other-day cycle from an anchor date) and ZS events (fixed weekdays or ASAP 71.5h chain) across a date range, skipping dates that already have an event.
- **Server Events**: Repeating game-wide events (Ironclad Vehicle, Zombie Invasion, Rampage Bosses, General's Trial, Doomsday, and any custom entries) shown as banners at the top of each day column. Supports weekly, biweekly, and every-N-days recurrence.
- **VS Alliance Duel Themes**: The fixed 7-theme weekly cycle (Radar Training → Alliance Star) is shown per day automatically — no configuration required.
- **Season Tracking**: Each day column shows the current season day (e.g. S3 D47), derived automatically from the active season's start date. The day counter continues incrementing through archived seasons so historical weeks stay accurate.
- **Week Grid**: 4+3 two-row layout (Mon–Thu on row 1, Fri–Sun on row 2) giving each day enough space for event cards and action buttons. Collapses to a single-day swipe view on mobile — the existing ← / → week nav buttons navigate days on small screens.
- **Desert Storm Integration**: Friday columns automatically show the two active Task Force battle slots (pulled from the Storm page TF configuration and admin-configured slot times).
- **Week Image**: Generate a canvas-rendered PNG of any week in the site's dark theme — suitable for sharing in Discord. Includes server banners, VS themes, event levels, season days, and storm pills.
- **Day Card**: Generate a single-day PNG (Discord-friendly proportions) for any day in the currently viewed week.
- **Text Output**: Plain-text block for each day, formatted for pasting into chat or Discord.
- **Advanced Settings**: Storm battle slot times (Slot 1/2/3 → clock time) are configurable by admins via a dedicated section on the `/admin` page.
- **Permission-Gated Access**: Separate `view_schedule` and `manage_schedule` permissions.

### 🎖️ Officer Command
- **Responsibility Directory**: A living org chart of standing alliance functions, grouped by domain (e.g. Membership, Relations, War). Not a task manager — no completion states or due dates.
- **Category & Responsibility Management**: Admins can create, rename, and delete categories and responsibilities inline without leaving the page.
- **Assignee Tracking**: Assign one or more members to each responsibility; chips display name and rank. Members can be added via a searchable picker and removed individually.
- **Frequency Badges**: Each responsibility carries a Daily / Weekly / Seasonal frequency, displayed as colour-coded pill badges.
- **Client-Side Filtering**: Filter the directory by assigned leader using a dropdown, or by frequency (All / Daily / Weekly / Seasonal) using pill-shaped filter chips — no round-trips to the server.
- **Drag-to-Reorder**: Categories and responsibilities within a category can be reordered by drag-and-drop; order is persisted server-side.
- **Permission-Gated Access**: Separate `view_officer_command` (R1–R5 default) and `manage_officer_command` (R4–R5 default) permissions control who can view vs. administer the directory.

### 🎯 Recruiting
- **Former Members**: Instead of deleting members and losing their history, officers can archive them (rank `EX`). Archived members remain in all historical records — train logs still show their name, VS point history is preserved. Officers with `manage_members` can view former members on the Members page ("Former" filter chip) and on the Recruiting page with stats: last known power, total train runs conducted, and last VS week active.
- **Reactivate**: Former members can be restored to an active rank (R1–R5) from the Recruiting page, re-joining the roster immediately.
- **Prospect Tracking**: Officers can log potential recruits with fields for in-game name, server, current alliance, power, Total Hero Power, rank in their alliance, recruiter, first contact date, notes, and status (Interested / Pending / Declined).
- **Officer Notes**: Each active member now has an internal notes field visible only to officers with `manage_members`. Notes are displayed in the member edit modal and never shown to standard members.
- **Permission-Gated Access**: `manage_members` controls archive/reactivate and viewing former members. Separate `view_recruiting` and `manage_recruiting` permissions (R4–R5 default) gate the Recruiting page and prospect CRUD. Hard-deleting archived members is admin-only.

### 🏆 Season Hub
A season-scoped tracking and reward distribution system for structured in-game competition seasons.
- **Season Management**: Create and archive seasons with configurable parameters — week count, key event name and attendance requirement, participation tier thresholds, and reward tier slot counts. Future-dated seasons are created in an upcoming state and activate automatically when their start date arrives. R5-only: edit or delete any non-active season.
- **Rankings Tab**: Sortable member standings showing participation percentage (with a per-week dot grid), contribution percentage (relative to the top contributor), key event attendance count, class tag (Active Member / At Risk / Dead Weight), and assigned reward tier. Visible to all members; R1–R3 see only their own row.
- **Participation Tab** *(R4/R5)*: Log weekly scores per member using configurable score levels (e.g. FULL / PARTIAL / ABSENT) and a key event attendance counter. Scores are saved per week with bulk-save and week navigation.
- **Contributions Tab** *(R4/R5)*: Manually enter season contribution totals across four categories (Mutual Assistance, Siege, Rare Soil War, Defeat) with week-level granularity. The season-end snapshot (week 0) is used as the canonical tie-breaker for rankings; weekly tracking is optional.
- **Rewards Tab** *(R4/R5 view, R5 assign)*: Assign reward tiers (Alliance Leader, Core, Elite, Valued) to members with an audit trail of who assigned what and when.
- **Season Mail**: Season-specific mail templates are stored in the Comms hub and scoped to the season. Full create, edit, delete, and variable-fill copy work from Season Hub directly — no need to leave the page.
- **Permission-Gated Access**: `view_season_hub` (R1–R5), `manage_season_hub` (R4–R5), `manage_season_rewards` (R5) — all configurable via the permissions matrix.

### 📬 Alliance Communications
A central hub for all alliance-wide mail templates, announcements, and reference resources.
- **Mail & Announcement Templates**: Create reusable templates organised into free-form, collapsible categories (e.g. Desert Storm, Policy, Reminder). R4/R5 manage; R3 view and copy.
- **Variable System**: Embed `{variable_name}` placeholders — you're prompted to fill them in when copying. Add a type prefix for a specific input: `{time:var}` (24h time picker), `{date:var}` (date picker), `{dayofweek:var}` (day-of-week dropdown), `{number:var}` (numeric field), `{multiline:var}` (text area). Use `{{` and `}}` for literal braces that won't be treated as variables.
- **System Variables**: Templates can declare variables pre-filled by integrations (e.g. the Storm page supplies `task_force`, `battle_time`, and `group_assignments` for the DS battle mail). Pre-filled variables are never shown in the fill-in modal.
- **Unified Season Mail**: Season-specific templates are stored in the same table, scoped by season. They appear in Season Hub for in-context editing and in Comms under their season category — one system, two entry points.
- **Resources Tab**: Store named external links (guides, spreadsheets, infographics) with optional descriptions for quick alliance-wide reference.
- **Search & Browse**: Live search filters across titles and content. When no search is active, templates group into collapsible category sections with session-persistent open/closed state.
- **Permission-Gated Access**: `view_comms` (R3–R5 default) and `manage_comms` (R4–R5 default), configurable via the permissions matrix.

### ⚖️ Member Accountability
- **Tag System**: Each active member is automatically tagged as Reliable, Needs Improvement, or At Risk based on their current active strike count. The strike thresholds for each tag are configurable in Alliance Settings.
- **Hybrid Strike System**: VS performance is auto-flagged against the configurable weekly minimum (set in Alliance Settings). Officers review flagged members and add strikes with one click — duplicate VS strikes for the same week are blocked. Storm no-shows and train no-shows are manually logged by officers post-event; all other strikes can be added with a free-form category, reason, and optional reference date. The three built-in categories (VS Below Threshold, Train No-Show, Storm No-Show) are always available; selecting Manual reveals a text field for any custom category (e.g. "Diplomacy Violation"). Custom categories persist in the database and appear in the dropdown for all officers on future visits.
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
- **Automated Data Extraction**: Drag and drop up to 100 game screenshots at once to automatically extract VS Points or Power updates.
- **Intelligent Pipeline**: The microservice automatically detects the screenshot type by analyzing colored UI tabs, groups them into buckets, and dynamically stitches them into vertical towers to bypass API limits and retain razor-sharp text.
- **Hybrid State Machine Parsing**: Overcomes vertical text-flow layout issues natively by intelligently pairing player names with valid scores while filtering out UI noise.
- **Validation UI & Machine Learning**: OCR results are held in a "Preview & Confirm" modal. Administrators can manually map unresolved scans to existing members and save the pairing as an `ocr` alias, teaching the Alias Engine to automatically correct that specific visual artifact in all future uploads.
- **Two OCR Backends**: The app ships two backends, switchable in Admin Settings:
  - **Cloud** (default): Sends images to Google Cloud Vision via an OIDC-authenticated Cloud Run worker. Fully automatic screen-type detection. Requires GCP credentials configured in Admin Settings.
  - **Local**: Runs a [PaddleOCR](https://github.com/PaddlePaddle/PaddleOCR) sidecar (`lastwar-ocr-service:local`) in Docker with no external dependencies. The user selects the image category manually per batch because PaddleOCR's English model cannot reliably detect Last War's stylised headers. Enable by selecting "Local" during `install.sh` / `update.sh`, or by setting `OCR_BACKEND_MODE=local` in `.env`.
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
- `OCR_BACKEND_MODE` - Set to `local` to use the PaddleOCR sidecar instead of Google Cloud Vision. Defaults to `cloud`. Also requires `COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml`.

## Default Login Credentials
- **Username**: `admin`
- **Password**: `admin123`

⚠️ **Important**: The system will force you to change this immediately upon first login!

---

## Credits & Acknowledgements

This project originated as a fork of [`vervelak/lastwar-alliance-manager`](https://github.com/vervelak/lastwar-alliance-manager). The original repository provided the foundation that this project was built upon, and we are grateful to its author for starting it. The codebases have since diverged significantly — features, architecture, and deployment have all evolved independently — but the original work deserves full credit for getting this started.

---

## Security Architecture

- **Encrypted Credentials**: External API credentials (like GCP Service Accounts) are symmetrically encrypted at rest using AES-GCM. The application employs strict memory hygiene, zeroing out sensitive plaintext buffers immediately after cryptographic operations or API transmissions to prevent memory scraping.
- **Isolated Internal Networking**: The Go application and the Collabora document server communicate exclusively over a private Docker bridge (`lastwar-net`), preventing external data exposure.
- **Strict CSRF Protection**: All mutating endpoints (POST/PUT/DELETE) are protected by robust Cross-Site Request Forgery tokens integrated natively into the JS fetch interceptors.
- **Content-Security-Policy**: The automated Caddy setup configures strict CSP headers on both domains. The main app uses `frame-src` and `connect-src` to restrict iframes and WebSocket connections exclusively to your Collabora subdomain. The Collabora server uses `frame-ancestors` to ensure it can *only* be embedded within your Alliance Manager domain.
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