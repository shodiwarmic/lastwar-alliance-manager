# Changelog

Every released version of the Alliance Manager, newest first, each entry linking the pull request
that carried it.

How versions are chosen and cut is in [docs/RELEASING.md](docs/RELEASING.md); how to take one is
in [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#6-update-procedure). In short: a **patch** or **minor**
needs only a new image (`docker compose pull`), while a **major** changes something outside the
image and needs a terminal run of `scripts/update.sh`.

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
