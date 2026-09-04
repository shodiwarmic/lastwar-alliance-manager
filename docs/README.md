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

## Third-party references live in the private docs repo

References compiled from someone else's service or documentation are **not** ours to
publish, and this repository is public — so they are not kept here at all. They live in
the private `lastwar-private-docs` repo, as `reference-*.md`, where they are actually
version-controlled and backed up.

- `LASTRANK_API_REFERENCE.md` — notes on the unofficial `lastrank.fun` `/v1/` API, written
  during the LastRank integration. **Moved to `lastwar-private-docs` as
  `reference-lastrank-api.md` on 2026-09-03.** It was previously kept here and gitignored,
  which meant it existed on one machine and was backed up nowhere. The `.gitignore` entry for
  its old path is retained deliberately, as a guard against it being re-added to a public repo.

  For day-to-day code changes, the safe-to-publish subset is restated in the "LastRank
  integration" section of `CLAUDE.md`; go to the private repo for endpoint and
  response-shape detail.

When you acquire a new third-party reference, put it in the private repo rather than here.
