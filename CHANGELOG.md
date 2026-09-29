# Changelog

Every released version of the Alliance Manager, newest first, each entry linking the pull request
that carried it.

How versions are chosen and cut is in [docs/RELEASING.md](docs/RELEASING.md); how to take one is
in [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#6-update-procedure). In short: a **patch** or **minor**
needs only a new image, while a **major** changes something outside the image. From v2.0.0 on,
`./scripts/manage.sh update` takes either kind; before it, a major needed a terminal run of
`scripts/update.sh`.

## v2.0.0 — 2026-09-28

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
