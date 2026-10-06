# README screenshots

`capture.mjs` takes the screenshots in `docs/img/` from a **throwaway local instance** seeded with
the fictional demo alliance — never from the public demo (shared, writable, and on whatever
version was last deployed) and never from a real database. The list of shots, the widths, the
themes and the date are in `shots.json`. Run by hand; not part of CI.

```bash
# from the repository root
go run ./cmd/demo-seed --db /tmp/shots.db --uploads /tmp/shots-up --password shots-only-pass \
  --today "$(jq -r .today tests/screenshots/shots.json)"
DATABASE_PATH=/tmp/shots.db STORAGE_PATH=/tmp/shots-up go run ./cmd/server &
cd tests/screenshots && npm install && PASSWORD=shots-only-pass node capture.mjs
```

`--only dashboard,members` retakes some shots. Afterwards look at every new image once for
anything that is not the demo alliance — there should be nothing, the seed being the only data.

## The date

`shots.json`'s `today` is the date the set was taken, and the seed is anchored on it. **The
server reads "today" from its own clock**, not from the seed — the current VS week, the
schedule's today, the Season Hub's current week — so the seed's anchor must be the day you
capture on. The script refuses otherwise.

The names and numbers do not depend on the date, so a set retaken on another day shows the same
alliance with its dates moved.

## When to retake

- **Retake the whole set** when a file every page loads changes: `layout.html` (including its
  `nav-links` / `theme-links` blocks) or the six assets it loads, `styles.css`, `global.js`,
  `theme.js`, `translate.js`, `csrf.js` and `modal-focus.js`. Move `today` to the capture day.
- **Retake one shot** when a JS or CSS file its page loads changes. Pages declare their assets as
  `{{asset "/x.js"}}` / `{{asset "/x.css"}}` in their own template, so the mapping can be computed
  from `git diff`. Shared components reach several pages: `choices-theme.css`, `job-progress.*`,
  `filter-panel.*`, `member-picker.*`, `color-picker.*`. On a day other than `today` that needs
  `--allow-other-day`, and the shot's dates will differ from the rest of the set.
- **Ignore Go-only changes.** No Go code builds HTML; the visible effects a Go change can have
  (an extra permission row on Settings, dashboard card order, default reward tiers) do not matter
  for a first-impression set.
