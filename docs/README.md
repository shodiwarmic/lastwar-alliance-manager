# Documentation

Design references, API specs, and planning artifacts that don't belong in the
application runtime but are worth keeping alongside the code.

Nothing in here is served by the app. Anything the app actually loads at runtime
belongs in `static/` or `templates/` instead.

## Tracked

- `vs-league-mockup.html` — design reference for the VS Duel League UI. Standalone;
  pulls in `/styles.css` when opened against a running dev server, but nothing in the
  app links to it. It previously lived in `static/`, where the catch-all file handler
  in `main.go` served it unauthenticated to anyone who guessed the URL.

## Local only (gitignored)

Third-party API references are kept here but **not** committed — they are not ours to
publish, and this repository is public. They are listed in `.gitignore` by their
`docs/` path.

- `LASTRANK_API_REFERENCE.md` — notes on the unofficial `lastrank.fun` `/v1/` API,
  written during the LastRank integration. See the "LastRank integration" section of
  `CLAUDE.md` for the parts of it that are safe to keep in the open.

When adding a reference that came from someone else's service or documentation,
default to gitignoring it and add a line here saying it exists.
