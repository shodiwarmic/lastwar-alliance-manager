# Changelog

Every released version of the Alliance Manager, newest first, each entry linking the pull request
that carried it.

How versions are chosen and cut is in [docs/RELEASING.md](docs/RELEASING.md); how to take one is
in [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#6-update-procedure). In short: a **patch** or **minor**
needs only a new image, while a **major** changes something outside the image. From v2.0.0 on,
`./scripts/manage.sh update` takes either kind; before it, a major needed a terminal run of
`scripts/update.sh`.

## v2.3.0 — 2026-10-07

The scanner app can now send every kind of game data the alliance manager stores, so new
scanner features no longer wait for a server release. Phone permissions now follow the
permission matrix live, alias changes are each recorded, and repeat uploads stop adding
duplicate history.

**Minor** — an image pull; nothing on the host changes, no `.env` needs editing, and there is no
database migration. Mobile-app logins survive the update.

- **New mobile endpoints** under `/api/mobile/` for VS Weekly Rank totals, hero power, squad
  power, HQ and profession level; member troop level, squad type and profession; confirmed roster
  changes (rank, rename, join, rejoin, leave); recruiting prospects; Season Hub contributions;
  participation boards from the event mails; VS Duel League weeks, days and brackets; train logs;
  and alliance power rankings. `GET /api/mobile/capabilities` tells the app what this server
  accepts and what this account may do. Officer judgement — notes, strategy, lineups, statuses —
  stays on the web.
- **Phone permissions are live.** A rank change, a deactivation or a permission-matrix edit now
  applies to a signed-in phone on its next request, instead of when its seven-day sign-in
  expires.
- **Each scanned reading needs the permission of the page that records it.** VS points need VS
  management, as on the VS page; power, kills and the other member figures need roster
  management, as everywhere on the web. If your permission matrix gives a rank one of these and
  not the other: a rank with only VS management can still upload VS points but its uploads stop
  saving power and kills; a rank with only roster management can now upload power and kills but
  not VS points. The default matrix (R4 and R5 hold both) is unaffected.
- **Every alias change is recorded**, one activity row each — from the VS import, the scanner,
  the Season Hub import, LastRank and the Members page. Saving a scanner or import alias no
  longer deletes other people's personal nicknames, and an OCR alias can no longer silently take
  over another member's global alias unless you can manage the roster. Renames keep the old name
  as a nickname on every path.
- **Repeat uploads no longer add duplicate history.** A power, kills or level reading equal to
  the member's last one is skipped and reported as unchanged, and a scan synced late is dated by
  when it was taken, not when it arrived.
- **Fixed: roster managers who aren't admins can add and remove global nicknames** on the Members
  page; only admins could.
- **Fixed: a train log for a member who no longer exists** is refused instead of half-saving and
  showing an error, and a log can't be dated in the future.
- **Fixed: the VS Duel League week save** answers the week it saved.
- **Desert Storm boards first recorded from a phone** offer the planner's lineup on the web, as
  a new board does.

## v2.2.0 — 2026-10-07

A fictional demo alliance and the public demo built on it, an administrator's preview of the app
as any rank, response compression, and a production install that refuses to start without a
session key.

**Minor** — an image pull; nothing on the host changes, and no `.env` needs editing — **provided
`SESSION_KEY` is set**, as every documented install does. An install running `PRODUCTION=true`
with no `SESSION_KEY` (or one shorter than 32 characters) now refuses to start; set one
(`openssl rand -hex 32`) before updating. Existing sessions, mobile-app logins and open
documents survive the update.

- **Preview as rank.** An administrator can see the app exactly as any rank does, from the user
  menu (**More** on a phone) or the Admin page's User Management tab; a banner on every page
  switches rank or exits. Starting, switching
  and ending a preview are recorded in the activity log.
- **The app compresses its own responses** (gzip for pages, scripts, styles and JSON), so an
  install reached without a reverse proxy is no longer served every asset full-size. Behind Caddy
  nothing changes.
- **`PRODUCTION=true` requires `SESSION_KEY`.** An unset key used to boot with a temporary one,
  logging everyone out on every restart. The mobile API's login is now rate-limited like the web
  login.
- **Local OCR mode's Upload page works without a Google Cloud key.** It reported the OCR pipeline
  as disabled unless a Cloud Vision key was stored too.
- **A demo alliance generator** (`cmd/demo-seed`) writes a complete, entirely fictional install —
  100 members, their history, a season, a Duel League, events and documents — and **demo mode**
  (`DEMO_MODE=true`) runs it as a public demo with one-click sign-in, a stand-in OCR service and
  document viewer (`cmd/demo-fixtures`), and a self-reset. Operators need none of it; see
  `docs/DEMO.md`.
- **Screenshots** in the README, of the demo alliance.
  ([#110](https://github.com/shodiwarmic/lastwar-alliance-manager/pull/110))

## v2.1.1 — 2026-10-03

A dependency replacement: the CSRF middleware moves from `github.com/gorilla/csrf` to
`filippo.io/csrf/gorilla`, for the public Go advisory
[GO-2025-3884](https://pkg.go.dev/vuln/GO-2025-3884), which has no fixed `gorilla/csrf` release.

**Patch** — an image pull; nothing on the host changes, and no `.env` needs editing.

- **CSRF protection now checks where a request came from, not a token.** A state-changing
  request from a browser is accepted when it is same-origin — read from the browser's
  `Sec-Fetch-Site` header, or by comparing `Origin` with `Host` where that header is absent —
  and refused otherwise. Requests from non-browser clients (the Android scanner, Collabora) are
  unaffected.
- **`TRUSTED_ORIGINS` is rarely needed now.** The app's own domain, `localhost` and a LAN address
  all work without an entry, because the page the app served is same-origin with it. Existing
  entries keep working. An entry written without a scheme (`app.example.com`) now trusts that
  host over HTTPS only; to trust a plain-HTTP origin, write it with its scheme
  (`http://192.168.1.50:8080`).
  ([#105](https://github.com/shodiwarmic/lastwar-alliance-manager/pull/105))

## v2.1.0 — 2026-10-01

Participation boards can be imported from the post-event mail's screenshots, the app now talks to
the OCR service through a versioned contract, and the VS upload stops losing Weekly Rank and
Donation screenshots.

**Minor** — an image pull; nothing on the host changes. **Needs OCR service v1.0.0 or later** for
the mail import (contract version 1, categories `alliance_exercise`, `zombie_siege`,
`desert_storm`). Against an older OCR service everything else works as before, and the import
says which release it needs instead of failing on the upload. A self-hosted local-OCR sidecar
gets it by pulling `:local` once v1.0.0 is released.

- **Import a participation board from screenshots** — on the recording screen, for Alliance
  Exercise (Marshal's Guard / Large Sandworm), Zombie Siege and Desert Storm. Ranks are kept as
  the mail shows them; anything the reading could not settle is flagged on its row; the mail's
  date and time suggest the event (and the Desert Storm task force). Saving is blocked while a
  member or a rank is on the board twice. A board's source (`manual` / `import`) is now recorded.
- **The OCR contract is versioned.** Every request names contract version 1; an answer in any
  other version is refused with a message naming both, and a refusal from the service is shown
  in its own words. Admin → About this install shows the OCR service's release, commit and
  contract version.
- **Fixed: Weekly Rank, Donation (Daily) and Donation (Weekly) uploads saved nothing** — each
  row failed with "no such column". A Weekly Rank screenshot now becomes each member's Saturday
  (the weekly total minus Monday–Friday) when Monday–Friday are all recorded, and the row says so
  when they are not; Donation screenshots are listed as not stored yet.
- **Fixed: a mid-week Weekly total no longer invents a Saturday** out of days not yet recorded
  (CSV import and screenshots alike).
- **Hardened: the VS import's commit accepts only real columns.** Field names sent by the
  browser were joined into the SQL statement as column names; anything but Monday–Saturday,
  power and kills is now refused before any SQL is built.

## v2.0.0 — 2026-09-29

Installs become versioned end to end. Until now the image followed a pinned release while
everything else on the server — the compose files, the scripts, the proxy configuration — was a
git clone that followed `main`. From this release those host files ship with each release as a
small download, and one command moves them and the image together. Fresh installs also stop
shipping with a default password.

**Major** — the host has to change, so this one is not an image pull.

**Existing installs: do not run `update.sh` for this release.** From your install directory run
these two commands, in this order:

```bash
git pull
./scripts/manage.sh migrate
```

This applies to every install from before v2.0.0, wherever it lives. (An install at `/opt/lastwar`
could not update itself at all — `update.sh` stopped with "are the same file" before fetching
anything — and this is the fix.) From then on, updates are `./scripts/manage.sh update`; the old
`update.sh` forwards to it if you run it by habit.

If `git pull` refuses because of local changes, run `git stash` first, then `git pull`; `migrate`
saves your changes and any stash as patch files under `/var/backups/lastwar/migrate_<timestamp>/`
and tells you where.

**An install with no `.git`** (copied over by SCP): download the asset and take its scripts out in
the install directory, then migrate the same way:

```bash
curl -fsSL -o host-files.tar.gz \
  https://github.com/shodiwarmic/lastwar-alliance-manager/releases/download/v2.0.0/host-files.tar.gz
tar -xzf host-files.tar.gz scripts/ && rm host-files.tar.gz
./scripts/manage.sh migrate
```

**An install still running the pre-Docker `lastwar.service`** is not migrated by this release.
Convert it to Docker with the v1.1.0 scripts first (`git checkout v1.1.0 && ./scripts/update.sh`,
whose update copies `/var/lib/lastwar` into the Docker install), then follow the steps above.

What `migrate` does, in place: takes a backup, removes the files git delivered that a server does
not need (source code, templates, docs), keeps everything git never delivered (`.env`, `data/`,
`uploads/`, your own files), writes the v2.0.0 host files, pins the image to v2.0.0, records the
install in `/etc/alliance-manager/installs.d/`, installs the nightly backup helper, and removes
`.git` last. Your data is not moved. If any step fails, the install is put back as it was.

- **Versioned host files** — each release carries `host-files.tar.gz` (and its checksum): the
  compose files, the scripts and the Caddyfile template. `./scripts/manage.sh update` moves an
  install to the newest release, or `--version vX.Y.Z` to a specific one — which is also how to roll
  back. It backs up first, fetches the new image before touching any file, removes files a release
  has stopped shipping (never yours), and restores everything if a step fails. Releases before
  v2.0.0 have no host files, so `--version` cannot target them; their images stay reachable by
  setting `APP_VERSION` by hand. `./scripts/manage.sh status` shows what is running;
  **Admin → Security & API → About this install** now shows the host files' release beside the
  image's.
- **Install anywhere** — a fresh install is a download into any directory, not a clone:
  `curl` the asset, unpack it, run `./scripts/install.sh`. The installer checks the host first —
  OS, Docker, free ports, DNS, disk, memory — offers to fix what it can, and runs unattended with
  `--non-interactive --domain …`. Every prompt has a flag; see `--help`.
- **No default password** — a new install has no account. The app writes a one-time setup key
  to `data/setup-key`; every page leads to a setup screen that takes the key, creates the first
  administrator, and asks for the alliance's name, tag and server. The key is valid 24 hours and
  works once. **Existing installs keep their accounts** and never see the setup screen.
- **Login rate limits can no longer be dodged** — the client address the per-IP limits key on was
  read from a header any client could set, so a script could get a fresh set of attempts per
  request. It is now read from the right end of `X-Forwarded-For`, trusting as many entries as
  `TRUSTED_PROXY_COUNT` says (default 1 when `PRODUCTION=true`, else 0); `migrate` writes `1`
  on installs running `PRODUCTION=true`. `X-Real-IP` is no longer read.
- **Ports published on 127.0.0.1** — the app (8080) and Collabora (9980) are reachable only
  through the reverse proxy on the same host, because Docker's published ports bypass `ufw`. A
  LAN install reached without a proxy sets `BIND_ADDR=0.0.0.0` in `.env`.
- **One Caddyfile** — the proxy configuration is one template with a revision number;
  `manage.sh update` re-renders `/etc/caddy/Caddyfile` (keeping a dated backup) only when a
  release ships a newer revision. `migrate` re-renders it once, since every older install's file carries no revision. The
  rendered file no longer has a `www.` redirect block, which asked Caddy for a certificate the
  documented DNS records cannot satisfy — add it back by hand if your DNS has a `www` record.
- **Nginx is manual-only** — the installer no longer configures nginx. Existing nginx installs keep
  working; new ones use `./scripts/install.sh --proxy none` and the nginx example in
  [DEPLOYMENT.md](docs/DEPLOYMENT.md). Keep its `X-Forwarded-For` line.
- **Backups** — nightly backups now cover every registered install, named after it
  (`nightly_<name>_<timestamp>.db`, kept 7 days), separately from the pre-update backups
  (newest 10 kept), and are written readable by root only. The pre-update tar no longer includes
  `data/` (the database has its own consistent backup beside it) but still includes `uploads/`.
- **Updates no longer ask questions** — settings an old `.env` lacks get their defaults
  (`OCR_BACKEND_MODE=cloud`); the old one-time prompts are gone.
  ([#102](https://github.com/shodiwarmic/lastwar-alliance-manager/pull/102))

## v1.1.0 — 2026-09-26

Event participation: record who took part in an event, and how, from the ranked list the game
mails afterwards — and let the app suggest the strikes that board implies, without ever filing one
on its own. Desert Storm battles become real events on the schedule so they can be recorded too,
and strike categories get a managed list.

Image only — take it with `APP_VERSION=v1.1.0` in `.env`, then `docker compose pull && docker
compose up -d`. Three database migrations run on start (079–081); they add tables and seed rows,
and change no existing strike or event. After upgrading, **R4 and R5 hold the two new
participation permissions**; give them to other ranks in Settings → Permissions if you want.

- **Event participation** — import an Alliance Exercise, Zombie Siege or Desert Storm board from a
  CSV (or add members by hand from a member search). A name is matched only when it is exactly a
  member's name or alias (a leading alliance tag is ignored) — nothing is guessed — and anyone
  unmatched gets a picker. A member on the board twice blocks saving. On a phone each row is two
  lines instead of a sideways-scrolling table. Saving lists the strikes the event's own rule implies — someone missing from an Alliance
  Exercise, anyone at 0 waves in a Zombie Siege, a starter missing from a Desert Storm (never a
  sub). Each can be struck, excused with a reason, or dismissed. Recording is optional and nothing
  nags about events nobody recorded. Boards are reviewed on a new **Participation** tab on the
  Accountability page and on each member's page, and every member sees their own under **My
  Participation** on their Profile. Schedule cards get a **Record** link and a board badge.
- **Desert Storm battles on the schedule** — one event per task force, generated every Friday
  from the Desert Storm page's setup (tick **Generate Desert Storm**), or added by hand with the
  task force chosen. Desert Storm is always on a Friday, so no other date can be saved. They replace the display-only Friday entries everywhere the schedule is drawn,
  exported and announced. The Storm page gets **Record last battle**, which prefills the
  battle's roles from the planner.
- **Storm Attendance is carried over and retired** — every date logged on the old Storm
  Attendance screen becomes a recorded Desert Storm battle with the same attended / no-show /
  excused marks. The old tab is replaced by Participation. The old table is kept for one release
  as a safety net and will be removed in the next.
- **Managed strike categories** — categories are a list edited from the Strikes tab
  (**Categories**): rename, reorder, add, deactivate, delete unused ones. Typing a new category
  into the strike form is gone, and categories already in use on your install are carried over
  exactly as spelled.
- **Desert Storm planner fixes** — a group with three or more buildings showed most of its
  buildings as empty (and a save from that view would have written them back empty); a lineup
  save that failed part-way reported success with members missing. Both are fixed, and the planner
  no longer shows raw database errors.
- **Smaller changes** — the Schedule, Accountability and member pages remember their tab in the
  address (`/accountability#report`), buttons on those pages carry icons and shrink to icons on a
  phone, the member page's tables scroll sideways on a phone instead of running off the edge,
  deleting a member is all-or-nothing, and a season delete keeps any calendar event that has a
  participation board.
  ([#100](https://github.com/shodiwarmic/lastwar-alliance-manager/pull/100))

## v1.0.1 — 2026-09-20

A single bug fix: server events could not be saved. v1.0.0 shipped with the fault, so every
install on that version is affected.

Image only — take it with `APP_VERSION=v1.0.1` in `.env`, then `docker compose pull && docker
compose up -d`. Nothing on the host changes, and `scripts/update.sh` writes the pin on its next
run.

- **Server events can be saved again** — on the Schedule page's Settings tab, adding or editing a
  server event failed every time with "Network error", no matter what was entered. The save was
  never actually attempted, so nothing reached the server and nothing was recorded anywhere;
  deleting a server event was unaffected. Officers can now create and edit server-event windows
  normally, including setting the anchor date that makes a window's occurrences appear on the
  calendar.
  ([#96](https://github.com/shodiwarmic/lastwar-alliance-manager/pull/96))

## v1.0.0 — 2026-09-20

The first tagged release. It changes nothing about the application itself; it marks the point
from which this install is versioned — published under tags that never move once released,
pinnable and roll-back-able through `APP_VERSION` in `.env`, and able to tell you which build it
is running.

Existing installs need do nothing: `scripts/update.sh` writes the pin on its next run. Taking
this release by hand is `APP_VERSION=v1.0.0` in `.env`, then `docker compose pull && docker
compose up -d`.

- **Releases and versioned installs** — version and commit stamped into the binary and shown on
  the Admin page, tag-triggered publishing (`vX.Y.Z` / `vX.Y` / `latest`, with `main` publishing
  `edge`), multi-architecture images for amd64 and arm64, a compose pin that install.sh and
  update.sh maintain, and two CI guardrails: a release-level check that a patch or minor changes
  nothing outside the image, and a check that every environment variable the app reads reaches
  `.env.example`.
  ([#95](https://github.com/shodiwarmic/lastwar-alliance-manager/pull/95))
