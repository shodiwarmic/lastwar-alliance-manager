# Copilot instructions

**The project guide is [`CLAUDE.md`](../CLAUDE.md) in the repository root.** Read it before
suggesting changes — it is written for AI coding assistants generally, not for one vendor, and it
carries the conventions and the non-obvious traps that this codebase will otherwise lead you into.

Quick orientation, because the details below are the ones most often guessed wrong:

- **Go backend** (gorilla/mux, gorilla/sessions, `modernc.org/sqlite` — pure Go, `CGO_ENABLED=0`),
  Goose migrations, `html/template` server-rendered pages. **There is no Node toolchain and no
  build step**; the frontend is vanilla JS and CSS served straight from `static/`.
- **One database connection** (`db.SetMaxOpenConns(1)`). Any query issued while another cursor is
  open deadlocks the whole process, silently. Read everything you need before opening a cursor.
- **No inline `<script>` in templates** — the production CSP is `script-src 'self'`, so an inline
  block is silently blocked. Pass configuration via `data-*` attributes instead.
- **No `alert()`, `confirm()` or `prompt()`** — use `showToast` / `showConfirm` from
  `static/global.js`.
- **Never build DOM from HTML strings** — `createElement` + `textContent`.

`CLAUDE.md` explains each of these properly, along with the migration, permission, activity-log
and asset-versioning conventions a new feature has to follow.
