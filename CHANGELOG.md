# Changelog

Every released version of the Alliance Manager, newest first, each entry linking the pull request
that carried it.

How versions are chosen and cut is in [docs/RELEASING.md](docs/RELEASING.md); how to take one is
in [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md#6-update-procedure). In short: a **patch** or **minor**
needs only a new image (`docker compose pull`), while a **major** changes something outside the
image and needs a terminal run of `scripts/update.sh`.

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
