# Alliance Manager — Claude Code Guide

## Stack
- **Backend**: Go, gorilla/mux, gorilla/csrf, gorilla/sessions, SQLite (`modernc.org/sqlite` — pure Go, no CGO)
- **Build**: `CGO_ENABLED=0` (no C compiler required in the build environment; see `Dockerfile`)
- **Migrations**: Goose (`-- +goose Up` / `-- +goose StatementBegin` headers required)
- **Frontend**: Vanilla JS, no build step. CSS custom properties (`var(--name)`) throughout.
- **Templates**: Go `html/template`, parsed as `layout.html` + page template pairs

## Adding a new feature — checklist

1. **Migration** — name it `NNN_feature_name.sql` where NNN follows the last file in `migrations/`. Check before assuming.
2. **Permissions** — add `FieldName bool \`json:"key_name"\`` to `RankPermissions` in `models.go`, and add a `PermissionRow{Key: "key_name", Label: "View"/"Manage"/etc.}` to the appropriate `PermissionGroup` in `PermissionGroups` in `models.go` (or add a new group). **No migration required.** No SELECT/Scan update, no admin shortcut update, no JS change — all handled automatically.
3. **Routes** — register in `main.go` following the existing pattern. UI page routes go in the `pages` map and `pagePermissions` map.
4. **Handler file** — one file per feature, e.g. `handlers_feature.go`.
5. **Template** — `templates/feature.html`. Define `header_text` (page-specific title, not the app title), `head_tags`, `content`, `scripts`, and `modals` blocks. All modals go in `{{define "modals"}}` — not inside `{{define "content"}}`.
6. **CSS** — `static/feature.css`, linked via `{{define "head_tags"}}` as `<link rel="stylesheet" href="{{asset "/feature.css"}}">`. The `{{asset}}` wrapper is required — see "Static asset cache busting". Never embed `<style>` blocks in templates.
6a. **Global utility check** — before adding any CSS to your page file, check if `styles.css` already provides what you need: `.card`/`.card-header`, `.data-table`, `.filter-chip`, `.tab-toolbar`, `.status-msg`, `.badge-*`, `.btn`, `.form-input`, `.tab-bar`, `.tab-btn`. For metric tiles use a grid of `.card`s (see DESIGN_STANDARD.md → Cards), not a bespoke `.stat-card`. Page CSS is for page-specific layout only.
7. **JS** — `static/feature.js`, loaded in `{{define "scripts"}}` as `<script src="{{asset "/feature.js"}}"></script>`. The `{{asset}}` wrapper is required — see "Static asset cache busting".
8. **Activity log** — call `logActivity` for every write operation (see section below).

## Activity logging

Every handler that creates, updates, or deletes data must call `logActivity`. The signature is:

```go
logActivity(userID int, username, action, entityType, entityName string, isSensitive bool, details ...string)
```

**Actions**: `"created"`, `"updated"`, `"deleted"`, `"archived"`, `"unarchived"`, `"imported"`, `"accepted"`, `"deferred"`, `"deactivated"`, `"reactivated"`, `"reset"`

> `"reset_password"` is retired — no handler emits it since the random-password flow was
> removed. Historical rows keep it, which is harmless: `activity.js` renders actions verbatim.

**`isSensitive`**: `true` for user accounts, permissions matrix, settings, credentials, and invite events. These are hidden from non-admin users on the activity page.

**`details`**: optional human-readable change summary. For updates, build a field-level diff and pass it as a single joined string:

```go
var changes []string
if old.Name != new.Name {
    changes = append(changes, "name: "+old.Name+" → "+new.Name)
}
// ... other fields ...
logActivity(userID, username, "updated", "entity_type", new.Name, false, strings.Join(changes, "; "))
```

For updates, fetch the old values **before** the UPDATE/Exec call, then compare after.

**Batching**: consecutive `"created"` calls for the same `entity_type` by the same user within 15 minutes are automatically merged (count increments). All other actions always create a new row.

> **Exempt from batching**: entity types listed in `neverBatched` (`activity.go`) always get
> their own row — currently `password_reset_link` and `invite`. Batching overwrites
> `entity_name` with the most recent value and only bumps a counter, so three reset links
> in a row collapsed to one row naming only the last recipient. For anything that grants
> credentials or access, the audit trail has to answer "who was given access, and by
> whom" — add the entity type to `neverBatched` rather than accepting the merge.

**`entity_type` values** (use these exact strings — they map to human labels in `activity.js`):
`member`, `alias`, `user`, `prospect`, `ally`, `agreement_type`, `train_log`, `eligibility_rule`, `oc_category`, `oc_responsibility`, `oc_assignee`, `award_type`, `awards`, `file`, `file_tag`, `schedule`, `storm_assignments`, `storm_config`, `storm_group`, `invite`, `password_reset_link`, `vs_points`, `power_records`, `permissions`, `settings`, `credentials`, `accountability_strike`, `storm_attendance`, `poll_template`, `poll_instance`, `lastrank_sync`, `lastrank_review`, `season_reward_tier`

When adding a new entity type, also add it to the `ENTITY_LABELS` (and `ENTITY_LABELS_PLURAL` if applicable) maps in `static/activity.js`.

## OCR backend (cloud vs local)

Two backends ship in this repo. The active one is stored in
`settings.ocr_backend_mode` (added in migration 032) and surfaced to
templates via `PageData.OCRBackendMode`.

| Mode | When to use | Picks the screen | Requires |
|---|---|---|---|
| `cloud` (default) | Hosted deployment | Auto-detects | GCP credentials in DB + Vision API enabled |
| `local` | Self-hosted, no Cloud Vision | User picks per batch | The `lastwar-ocr-service:local` Docker image (PaddleOCR sidecar) |

`install.sh` and `update.sh` prompt the operator to opt in to local mode
on first install (or once on update for pre-existing installs). When
local is selected, both scripts:
1. Append `OCR_BACKEND_MODE=local` and `COMPOSE_FILE=docker-compose.yml:docker-compose.local-ocr.yml` to `.env`.
2. Set `settings.ocr_backend_mode = 'local'` and default `cv_worker_url = 'http://ocr-local:8080'` in the DB.
3. The next `docker compose up -d` brings up the `ocr-local` sidecar service defined in `docker-compose.local-ocr.yml`.

Handlers should call `ProcessImages(ctx, files, category)` (in
`image_processing.go`) which dispatches to either `ProcessImagesViaWorker`
(cloud, OIDC-authenticated) or `ProcessImagesViaLocalWorker` (plain HTTP)
based on `LoadOCRBackendConfig()`. Don't hand-roll the dispatch in new
handlers.

These return `(CVWorkerResponse, *OCRDiagnostics, error)`. The worker
response is read by `decodeWorkerResponse`, which expects the
`{"results": {...}, "diagnostics": {...}}` envelope (a missing top-level
`results` key is an error). The `diagnostics` block is
persisted opaquely as `diagnostics.json` in the OCR archive (alongside
`response.json`) and parsed into the lean `*OCRDiagnostics` only to build the
one-line activity-log summary via `summarizeOCRDiagnostics` (`nil` /`""` when
the OCR service returns no diagnostics — always nil-check). It is treated as an
opaque blob for storage, so OCR-side schema changes need no Go change.

### OCR service deploy ordering

The app requires `lastwar-ocr-service` to be running the response-envelope
format (introduced alongside OCR diagnostics — `decodeWorkerResponse` treats a
missing top-level `results` key as an error). Deploy accordingly:

- `lastwar-ocr-service` must be deployed **before or simultaneously with** the
  Alliance Manager app.
- Rolling `lastwar-ocr-service` back to a pre-envelope version while the app is
  on the current version will cause OCR imports to fail.
- The backward-compat shim that tolerated the old flat response was
  intentionally removed in Epic 42.

`category` is required for local mode and ignored for cloud mode.
Allowed values are the same as the OCR service's `VALID_CATEGORIES`
list — `monday`–`saturday`, `weekly`, `power`, `kills`,
`donation_daily`, `donation_weekly`, plus the 12 `<category>_<period>`
keys for Alliance Contribution. The upload UI's "Image Category"
dropdown enumerates them.

Why local mode requires manual selection: PaddleOCR's English model
can't reliably read Last War's stylised header text
(`STRENGTH RANKING`, `ALLIANCE CONTRIBUTION RANKING`), so the page-
identification stage isn't trustworthy. Body OCR (player rows, scores,
tab labels) is comparable to Cloud Vision; that's enough for extraction
once the user supplies the screen + tab.

## Mobile API (`/api/mobile/*`)

Four endpoints serve the Android scanner (`lastwar-android-scanner` repo). All routes are wrapped in `mobileBearerMiddleware` — JWT bearer token in the `Authorization` header, claims fetched via `getMobileClaims(r)` inside handlers.

| Method | Path | Handler | Purpose |
|---|---|---|---|
| POST | `/api/mobile/login` | `mobileLogin` | Issue JWT |
| GET | `/api/mobile/members` | `getMobileMembers` | Roster + aliases for client-side resolution |
| POST | `/api/mobile/preview` | `mobilePreview` | Resolve scanned entries to members; returns matched/unresolved split |
| POST | `/api/mobile/commit` | `mobileCommit` | Persist confirmed scan data + optional alias mappings |

### Roster shape (`MobileMember` — see `models.go`)

Both `getMobileMembers` and `mobilePreview` return members in this shape:

```json
{
  "id": 42,
  "name": "ShodiWarmic",
  "rank": "R5",
  "aliases": [
    {"alias": "ShodiW", "category": "personal"},
    {"alias": "Shodi",  "category": "global"}
  ]
}
```

`aliases` is **scoped to the requesting user**: each entry is either the current user's `personal` alias, or any user's `global` / `ocr` alias. Other users' personal aliases are filtered out by the `LEFT JOIN` clause in `loadMobileRoster`:

```sql
LEFT JOIN member_aliases a
  ON a.member_id = m.id
  AND (a.user_id IS NULL OR a.user_id = ?)  -- ? = current user
```

If you add a new alias category that should be visible to mobile clients, update `loadMobileRoster` and the `MobileAlias` struct accordingly. Don't widen the `WHERE` clause to include other users' personals — the scanner uses these for on-device name disambiguation and including personals from other users would leak private mappings.

### Wire format for `/api/mobile/preview` entries

Scanner → backend payload is `{name, score, category}` per entry — **no `candidates[]` array**. The scanner runs its own crash-token disambiguation (using the cached roster + the same Exact → Personal → Global → OCR alias hierarchy as `resolveMemberAlias`) before sending. The backend's `resolveMemberAlias` runs once per received name as a final safety net, but cannot fix a wrong score because by the time the entry hits the API only one `(name, score)` pair survives.

This intentionally differs from the OCR-service path, which sends `candidates[]` because it has no roster access. Both paths converge on the same backend disambiguation rules — see the "Name resolution" section of `lastwar-screen-definitions/README.md` for the canonical algorithm both implementations must agree on.

### Activity logging

`mobileCommit` already calls `logActivity` for each VS / power / kill record write (`vs_points`, `power_records`, `kill_count` entity types — same as the web import path). New mobile endpoints that write data must do the same.

### Week date normalization

The server is authoritative on `week_date`. `mobileCommit` snaps every submitted value to the game-time VS-week Monday (UTC−2 fixed offset) via `normalizeToGameWeekMonday()` on ingest, overwriting whatever the client sent. Scanner clients need not compute the correct game-time Monday themselves — any Monday within ±3 days of the correct week is corrected server-side. (The `lastwar-android-scanner` repo should still compute it consistently so its local previews bucket the same way as the committed data.)

## LastRank integration (`/api/lastrank/*`)

Enrichment from the unofficial `lastrank.fun` `/v1/` API. All upstream calls go
through `lastrank_client.go`, which owns a single package-level
`rate.NewLimiter(1/sec)` (shared across all callers — the volunteer-run service
must never see more than 1 req/sec) and a 10s-timeout client. Wire structs there
use pointers for every nullable field and never leave the file; handlers in
`handlers_lastrank.go` translate to the app-facing payloads in `models.go`.
Never return a raw upstream error to the client — log with `slogLastRank` and
return a generic message.

Two phases, both manual-trigger only:
- **Phase 1** (`/preview` → `/commit`): one `fetchLastRankAlliance` GET, matched
  to the roster via `resolveMemberAlias`. Power/hero/HQ apply automatically; rank
  diffs and **name changes** (matched-via-alias, name ≠ roster primary) are
  review-only; unmatched names → global alias / rename / add; members absent or
  unranked on LastRank are offered for archive (default off). Member
  `lastrank_public_id` is captured here.
- **Phase 2** (`/player` per member, then `/finish`): browser-driven loop,
  oldest-`lastrank_synced_at` first so an interrupted run resumes. Each player
  refreshes kills + power + hero + HQ + avatar from the one record. `/player`
  writes are deferred-logged — `/finish` writes the single `lastrank_sync`
  activity row. Prospect lookups (`/prospect`, `/prospect/finish`) mirror this.

**NAP ladder sync** (`/api/allies/nap/*`) follows the same three-phase shape:
`/refresh` (one ladder call, writes the registry + history) → per-alliance member counts (member
counts are NOT on the ladder endpoint, only on the per-alliance detail record, so each costs its own
upstream call at ~1/sec) → `/finish` (one activity row). Phase-2 writes are deferred-logged. It gives
per-item progress, an interrupted run keeps what it wrote, and it never blocks one request for ~15s.

> **The bulk phase runs SERVER-SIDE** as the `nap_members` job (`jobs_nap.go`), not as a browser
> loop — as do the Members extended sync and the External Alliances gather. The three
> byte-identical `.lr-prog-*` / `.ext-prog-*` / `.nap-prog-*` CSS blocks were consolidated into
> **`job-progress.css`** (`.job-progress` / `.job-prog-row` / `.job-prog-name` / `.job-prog-status`,
> states `queued|active|done|skip|err`), driven by `JobProgress.attach()`. Use those classes for any
> per-item progress list — don't invent a fourth progress mechanism, and don't reintroduce the
> per-page prefixes. `/api/allies/nap/member` still exists as the single-alliance endpoint the job's
> `Step` shares with the HTTP path.

**The one sanctioned browser-driven loop** is the Scout Report's extended pass
(`static/alliance-report.js`). It is a deliberate exception to the server-side-jobs rule, for two
reasons that don't apply to any other flow:
1. `jobKind.New` takes only a `jobActor` — the framework has **no per-run target**, and every other
   runner derives its work from the DB. "Report on *this* alliance" can't be expressed without
   changing the framework.
2. `background_job_items.label` persists one row per item, which would write every opponent player's
   name into the database — exactly the data the report exists to *not* store.

It still reuses `job-progress.css`, and pacing is enforced server-side by `lastRankLimiter`, so the
client cannot outrun the politeness budget however fast it iterates. Don't "fix" it into a job.

**Fetch strategy** (`lastrank_client.go`): `GET /v1/players/{id}` is the cheap
cached read; `POST /v1/players/{id}/enrich` forces a slow live game re-pull
(separate 25s-timeout client). Bulk paths use `lastRankPlayerBulk` (GET, upgrade
to enrich only if `last_enriched_at` older than `lastRankEnrichMaxAge`=24h);
single prospect lookups use `lastRankPlayerFresh` (always enrich + GET fallback).
Never bulk-enrich the whole roster — it's slow and abusive to the volunteer service.

**Adding a global alias** uses `addGlobalAliasOverwritingOCR` (member_aliases has
no unique index): it deletes any same-named OCR/global alias first so global wins
over background OCR, leaving per-user personal aliases alone.

## Scout Report (`handlers_alliance_report.go`)

A member-by-member report on an **outside** alliance — the Scout Report tab on
`/external-alliances`. Two endpoints, deliberately asymmetric:

| | Route | Cost | Writes? |
|---|---|---|---|
| Basic | `POST /api/external-alliances/report` | 1 upstream request, whole roster | alliance stats only |
| Extended | `GET /api/external-alliances/report/player` | 1 per member | **nothing** |

**The member data is NEVER persisted.** It is scouting data about players in somebody else's
alliance; the app keeps no shadow roster of them. The rows live in the browser tab and are
exported from there. Adding a table for them would be the wrong fix for any feature request
that seems to want one.

**Three rules that must hold:**

1. **The roster fetch is POST, not GET** — it writes (registry stats, a history datapoint, an
   activity row), and gorilla/csrf only covers POST/PUT/DELETE. The per-player step is a pure
   read and stays GET.
2. **The extended pass uses `lastRankPlayerBulk`** — the shared bulk strategy: cheap cached
   GET, upgraded to a live enrich only when the record is older than `lastRankEnrichMaxAge`.
   Scouting on stale figures is worse than useless, because it invites planning against a
   version of the alliance that no longer exists.

   The cost is **variable and can be large**: a well-tracked alliance is nearly all cached
   GETs, while one nobody has looked at can need an enrich per member (25s ceiling each, plus
   the 1/sec limiter). That is contained by the pass being opt-in, cancellable mid-run, and
   reporting `enrich_status` per row so a 20-second row reads as "refreshed live" rather than
   as a hang. The handler ceiling is `allianceReportEnrichTimeout` (30s) — it must stay above
   `lastRankEnrichHTTP`'s 25s, or it would cancel the very re-pull it asked for.

   This is the one bulk path over players we do **not** own, so it is also the one most
   exposed to the volunteer service. Don't widen it — no auto-run, no scheduled variant, and
   never `lastRankPlayerFresh` (which enriches unconditionally).
3. **The alliance save NEVER mints a registry row.** `saveReportAllianceStats` resolves an
   *existing* row (by `lastrank_id`, then by tag — **backfilling `lastrank_id` before the
   write**, since `storeNAPAllianceSnapshot` resolves by that column alone) and otherwise saves
   nothing, reporting `in_registry:false` so the UI can offer to add it. Deliberately not
   `findOrCreateExternalAllianceTx`: looking an opponent up must not grow the registry. A
   same-tag row carrying a *different* `lastrank_id` is treated as untracked — tags are
   reusable, and retargeting it would overwrite an unrelated alliance's stats.

Our own alliance is handled by the `IsOwn` branch (Rule 2 → no registry row; the datapoint
lands in the `is_own` series), and the activity row is written **only when something actually
changed** — a report on unchanged numbers is a pure read.

> **`last_seen_at` is a SCAN timestamp, not player activity.** It is when lastrank scanned
> that player from the game — the as-of date of the data — while `last_enriched_at` is when
> the enrich endpoint was last *called* on them (a record of our polling, not of the game).
> Members of one alliance are scanned together, so their timestamps cluster within seconds of
> each other and of the alliance's own.
>
> The report column is therefore labelled **"Scanned"**, and there is deliberately **no
> "last active" filter** — one would let an officer write off a live player as dormant on the
> strength of scan scheduling. The `// game-side "last active"` comment in `lastrank_client.go`
> that seeded this misreading has been corrected; don't reintroduce that framing. If a genuine
> activity signal ever appears upstream, that is what such a filter should key on.

**Extended filters only exist once extended data does.** The profession / kills / origin chip
rows (`.rep-ext-row`) are hidden until the first extended row lands and are reset when a new
basic report starts — a stale extended filter left active over a basic report would silently
hide rows with no visible chip explaining why. Rows still awaiting their lookup legitimately
fail an extended predicate and drop out mid-run; that is correct, and it is the reason the
chips stay hidden beforehand rather than matching nothing.

> **`members[]` is sparse upstream, and that is not a bug.** `GET /v1/alliances/{id}` returns
> only players LastRank actually holds a record for — a function of who has been looked up
> there, not of the alliance's real size. Verified live 2026-08-18: our own alliance returned
> 99 members (`cur_member` 91 — it includes recently-departed players), while `Clts`
> (`cur_member` 67) returned **0** and `WARK` (24) returned **1**. `member_limit` does not
> change this, and `GET /v1/global/players?alliance_abbr=` agrees (0 rows for `Clts`), so
> there is no alternative endpoint to switch to — and no alliance-level enrich exists.
> The UI states this explicitly rather than rendering an empty table, because an unexplained
> empty result reads as a broken lookup and invites pointless retries. Don't "fix" it by
> hunting for another endpoint; do preserve the empty-state explanation.

**Alliance search: two strategies, one endpoint.** `GET /api/external-alliances/search` takes a
`scope` param, and both branches return `[]VSLeagueAllianceSearchResult`:

| `scope` | Upstream | Use |
|---|---|---|
| *(default)* | `/v1/global/alliances?search=&server_id=` | Registry add/edit modal — strict server, fuzzy name, carries power/kills |
| `any` | `/v1/search?kind=alliance` | Scout picker — the site's own relevance search, **every server**, no power |

**The scout picker must never assume our server.** VS Duel League is cross-server, so the
opponent an officer is looking for is usually *not* on our server — filtering to it hides
exactly the alliance they want. The two upstreams also rank differently: `/v1/global/alliances`
substring-matches names sorted by power, so searching `cROw` surfaces "Crowned Vengeance" and
"NeCROWmancers" above the real tag match, while `/v1/search` returns the tag hits the site's
own search box shows. `/v1/search` carries no power — that's why `Power` stays a nil pointer
rather than 0, and why a picked hit resolves its details on the follow-up by-id fetch.

Because a tag search routinely returns the same tag on 20 different servers (verified live:
`cROw` → 20 hits, 20 distinct servers), **the server number is the only disambiguator** — keep
it first in the picker's meta line, and keep `mapLastRankSearchHits` dropping `kind != "alliance"`
and id-less rows.

> Careless probing of `lastrank.fun` **will trip Cloudflare's bot challenge** (an HTML
> "Just a moment…" page, not JSON). Always send the `User-Agent` that `lastRankDo` sets and
> keep to ~1 req/sec — the limiter does this for app traffic, but hand-run `curl` checks
> bypass it entirely.

## Name matching — the folded fallback tier

`resolveMemberAlias` (`handlers_vs_import.go`) resolves in three tiers: exact name →
alias hierarchy (personal → global → OCR) → **accent-folded**. Tier 3 exists because
SQLite's `LOWER()` and the `NOCASE` collation are both ASCII-only, so a roster
`Pàcha` never matched an incoming `Pacha`. In the LastRank sync that miss cost
twice: the member landed in "Unmatched names" *and* in "Possibly left the alliance",
inviting an officer to archive an active member over a stray accent.

`foldName` (`namematch.go`) is the Go counterpart of `window.foldSearch`
(`static/global.js`) — NFD, drop `unicode.Mn`, lowercase. **Keep the two in sync**,
or client-side search and server-side matching disagree about what is the same name.

Two rules for tier 3:

1. **A folded key reaching two or more distinct members is NO match.** Guessing
   would silently attribute one player's stats to another; leaving the row unmatched
   puts it in front of an officer. `foldedNameIndex.lookup` enforces this.
2. **Build the index once per batch.** `resolveMemberAlias` is called once per
   incoming row inside loops over the whole roster, so a tier 3 that rebuilds per
   lookup is O(N²) queries on the single DB connection. Bulk callers use
   `buildFoldedNameIndex` + `resolveMemberAliasWithIndex(tx, name, userID, idx)`;
   plain `resolveMemberAlias` keeps the old signature and builds on demand (only on
   a tier 1/2 miss) for genuine one-shot callers. Existing bulk sites: the LastRank
   preview, `mobilePreview`, the OCR import, the contributions import, and the CSV
   import. `namematch_test.go` guards the no-rebuild contract.

Folding is **strictly additive** — it only runs after tiers 1 and 2 miss, so it can
turn a miss into a match but never change an existing match.

**Avatars** are hotlinked from the game CDN (`lastwar-cdn.akamaized.net` /
`lastwar-cdn.lastwarapp.net`) — built via `buildLastRankAvatar()` in `global.js`
with host failover. These hosts MUST be in the reverse-proxy CSP `img-src`
(`install.sh` for new installs; `update.sh` auto-patches the Caddyfile on
existing ones, keyed on the CDN host being absent). Without them avatars are
blocked in production (they work in dev because there's no proxy CSP) and fall
back to initials.

**Two rules that must hold for every history write from LastRank:**
1. **Staleness** — only insert if LastRank's capture date (`last_seen_at` /
   `captured_at`) is strictly newer than our latest `recorded_at` for that
   member+metric (`lastRankCaptureNewer`), compared as parsed `time.Time`, never
   as strings (ISO `T`/`Z` vs SQLite space form mis-sorts lexically). HQ never
   regresses (only apply if higher).
2. **Capture date as `recorded_at`** — inserted rows are stamped with the LastRank
   capture date (`lastRankCaptureToSQLite`), not the sync time, so the history is
   faithful and "stale never wins" falls out of the existing "latest by
   recorded_at" query for free.

**Datapoint provenance** — the history tables (`power_history`,
`hero_power_history`, `kill_history`, `squad_power_history`, `hq_level_history`,
`profession_level_history`) carry a
`source` column: `lastrank` | `ocr` | `csv` | `mobile` | `manual` (default
`manual`; pre-migration rows can't be reclassified). New write paths must stamp
their true source; `provenanceSource()` normalizes a client-declared origin. The
OCR-vs-CSV split is carried by a `source` field on the import commit payloads
(`upload.js`='ocr', `vs.js`='csv', roster `confirmMemberUpdates`='csv').

## History table source provenance

The `source TEXT NOT NULL DEFAULT 'manual'` column lives on these seven history
tables. Six are **member-level** — `power_history`, `hero_power_history`,
`kill_history`, `squad_power_history` (source added in `050_lastrank.sql`), and
`hq_level_history`, `profession_level_history` (created with the column in
`055_career_hq_history.sql`). One is **alliance-level** —
`alliance_stats_history` (`058_our_server_and_nap.sql`). It records how each row
was created:

| Value | Meaning |
|---|---|
| `manual` | Entered by an officer via the UI (also the default for pre-migration rows, which can't be reclassified) |
| `lastrank` | Synced from LastRank.fun (the per-player endpoint, or the per-server alliance ladder) |
| `ocr` | Extracted from an uploaded screenshot |
| `csv` | Imported via CSV file |
| `mobile` | Submitted by the Android scanner app |

Every new write path must stamp its true source; `provenanceSource()`
normalizes a client-declared origin. No other history/state tables carry a
`source` column — don't assume one on tables outside this list.

> **`alliance_stats_history` is keyed on `external_alliance_id`, not
> `lastrank_id`.** LastRank is *a source, not a required service*: a datapoint may
> equally come from OCR, CSV, mobile, or an officer typing it in, and none of those
> have a `lastrank_id` — keying on it would make those rows unstorable and
> contradict the `source` column. `lastrank_id` is a nullable reference attribute,
> exactly as migration `057` specifies for the registry itself. Every ingest path
> must therefore resolve its alliance into `external_alliances` first (via
> `findOrCreateExternalAllianceTx`), which mints the subject key.
>
> **Our own alliance is the one subject with no registry row** — see the Rule 2 note
> below — so its series is identified by `is_own = 1` with a NULL
> `external_alliance_id`, enforced by a `CHECK`. Beware: `INSERT OR IGNORE` (used for
> capture idempotency) silently swallows `CHECK` violations too, so assert the
> registry id is valid *before* inserting rather than letting `OR IGNORE` mask a bug.

> **Two writers, two clocks — and change-only datapoints.** The ladder refresh
> (`insertLadderStats`) appends a row per ladder capture, carrying `power_rank` /
> `kills_rank`, stamped `recorded_at = <ladder captured_at>`. The NAP member gather
> uses the per-alliance DETAIL endpoint and appends via `appendDetailDatapoint`,
> stamped `recorded_at = <detail last_seen_at>` with ranks left **NULL** — a rank is
> a position within one ladder capture, and the detail endpoint has no ladder to
> rank against.
>
> The detail path writes a datapoint **only when power, kills and member_count all
> differ from the temporally preceding row**. A point identical to its predecessor
> carries no information: the series already says the value was that and has been
> since, so recording it again just inflates the history at whatever cadence the
> gather happens to run and makes "when did this change?" harder to read.
>
> The detail path must never touch `external_alliances.lastrank_captured_at`,
> `power_rank` or `kills_rank` — see migration 058 for why mixing the two clocks
> strands a row's rank at NULL forever. It guards on and writes `lastrank_seen_at`.

## Our own alliance must never be in `external_alliances` (Rule 2)

`external_alliances` is a registry of **external** alliances. It feeds the VS League
opponent picker, the prospect source-alliance field, and ally prefill — so a row for
our own alliance would let an officer pick their own alliance as a VS opponent.

This is an invariant to **enforce**, not merely maintain: "stop inserting" is not the
same as "not present". It is upheld at four points:

1. `findOrCreateExternalAllianceTx` refuses to mint a row whose tag is ours (an ally or
   prospect carrying our tag still saves — it just gets no registry link).
2. `cacheExternalAlliance` refuses to cache us (reachable by pasting our own LastRank
   URL into the VS opponent field).
3. `updateSettings` scrubs the registry when the LastRank alliance **id** changes — the
   new identity may already be cached from when it was somebody else. Deliberately
   id-only: scrubbing by tag on a settings save would let a tag typo delete an innocent
   alliance's row.
4. The NAP refresh re-asserts the scrub as a backstop.

Use `isOwnAlliance(lastrankID, tag)` for the test and `scrubOwnAllianceFromRegistry` for
the removal. Both sides of any comparison must be non-empty — an empty configured tag
matching a blank upstream `abbr` would brand a *foreign* alliance as us. Note the scrub
**detaches** ally/prospect references before deleting, unlike `deleteExternalAlliance`,
which refuses with 409 when an ally references the row (blocking would strand the
invariant).

> **HQ level & profession level are history-only.** There is no `members.level`
> column (dropped in `055`; seeded into `hq_level_history` first) and no
> `members.profession_level` column. "Current" HQ / profession level is the
> latest row in `hq_level_history` / `profession_level_history`, derived via a
> `ORDER BY recorded_at DESC LIMIT 1` subquery in the roster and profile queries
> — same pattern as power/hero/kills. The `Member.Level` / `Member.ProfessionLevel`
> JSON fields are populated from those subqueries. Manual edits (member modal,
> My Profile, CSV import, prospect accept) and LastRank sync all append rows;
> HQ never regresses (only a higher value is recorded).

## Scheduled LastRank retrieval (`lastrank_schedule.go`)

Opt-in, off by default. A 15-minute ticker checks whether the current slot has been
crossed since the last **completed scheduled** run of each kind — derived from
`background_jobs`, so it self-heals across restarts with no extra state. A manual
run deliberately does NOT satisfy the schedule.

### The two numbers are coupled — do not tune one alone

The tick **interval** decides how often we look; the enrich **max age** decides, per
member, whether that tick actually re-pulls. At the 6h/21h default, ages of 6/12/18h
all fall under 21h, so only the 24h slot clears it:

| tick | age | enrich? | extended-sweep cost |
|---|---|---|---|
| 04:00 | 24h | **yes** | ~200 reqs, 10–40 min |
| 10:00 | 6h | no | **0 reqs** |
| 16:00 | 12h | no | **0 reqs** |
| 22:00 | 18h | no | **0 reqs** |

One enrich per member per 24h — a hard "refreshed within a day" guarantee — while
the 1-request alliance pull runs 4×/day so decisions reach the review queue within
6h. The legal band is `24 − interval < max_age ≤ 23`, computed by
`enrichMaxAgeBand` and enforced in `updateSettings`.

### Three timestamps, three questions

Conflating any two reintroduces a starvation bug:

| Column | Written | Used for |
|---|---|---|
| `lastrank_synced_at` | every attempt | **ordering** (always advances) |
| `lastrank_enriched_at` | only on `enrich_status: "fetched"` | **freshness filter** |
| `lastrank_attempted_at` | every per-player attempt | **scheduled backoff** |

`attempted_at` exists because Phase-1 commit paths also stamp `synced_at` — a commit
before a heavy tick would otherwise make the roster look freshly attempted. And
without it, a permanently `gated` member never advances `enriched_at`, stays due
forever, and is retried every tick — which is what stops the cheap ticks being
free. Prospects need no `attempted_at`: nothing else writes their `synced_at`.

**`TestScheduledSweepPlansNothingWhenNobodyIsDue` is the cost-model guard.** If the
pre-filter in `lastRankExtendedJob.Plan` regresses, every tick costs a full roster
of GETs and the tighter interval silently becomes more expensive than a looser one.

### Tiering

Scheduled Phase-1 (`jobs_alliance.go`) auto-applies **stats only** — staleness-gated,
provenance-stamped, append-only history that needs no human. Rank changes, renames,
unmatched names and archives always go to the queue and are **never** applied
unattended. It stamps `lastrank_public_id` but deliberately not `lastrank_synced_at`:
bumping that for the whole roster every 6h would scramble the extended sweep's
ordering. It resolves with `userID 0`, so no officer's private aliases influence an
unattended run.

## LastRank review queue (`lastrank_queue.go`)

Phase-1 **decisions** are durable: rank changes, name changes, unmatched names and
possible departures live in `lastrank_pending_changes` (migration 064) instead of
vanishing when the modal closes. Stats are deliberately NOT queued — power / hero /
HQ are staleness-gated, provenance-stamped, append-only history, so there is
nothing for a human to decide.

**Keyed on `(kind, subject_key)`.** A naive `(member_id, lastrank_public_id)` key
collides: `lastrankAllianceMember.PublicID` is a non-pointer `int`, so a missing
upstream id decodes to `0` and two unmatched entries without one would both key on
`('unmatched', 0, 0)`. Build keys with `lastRankSubjectKey` — the no-id case folds
the name through `foldName`, so a re-accented name can't mint a duplicate.

**Two kinds of "no".** `reconcilePendingChanges` re-opens a row when:
- the **fingerprint changed** (LastRank now proposes something different), or
- it was `deferred_once` and the **capture date advanced**.

Keying "not now" on the capture date *advancing* — rather than on any refresh —
means clicking Fetch twice in a minute doesn't evaporate the deferral. Proposals a
pull no longer makes are **deleted**: upstream withdrew them.

**Apply re-validates.** A queued proposal can be days old. Every apply goes through
the shared helpers in `lastrank_apply.go` (`applyRankChange` / `applyNameChange` /
`applyArchive` / `applyUnmatchedAction`), each of which reports whether it actually
changed anything. A proposal reality already overtook resolves as **superseded**,
not as a phantom success. Those helpers are shared with `lastRankCommit` on purpose
— two paths that both "apply a rank change" would drift, and the queue path is the
one nobody watches.

**Three alert surfaces**, all fed by `GET /api/lastrank/review/summary` and all
gated on `manage_members` — only someone who can act on a decision is told one is
waiting: the `lastrank-review` dashboard card (registered in `allowedCards` +
`CARD_META`), the Members-panel count badge, and a once-per-session login toast in
`global.js`. The toast reads its permission from a `data-*` attribute on
`#layout-config` rather than an inline `<script>`, which `script-src 'self'` would
silently block. Deliberately no nav badge — `nav-links` renders twice from one
block, so IDs there would collide.

**Unmatched names resolve one at a time.** Resolving one needs a per-row target
(which member, or "add as new"), and the request carries only one; a batch
containing an unmatched row is rejected rather than aliasing several different
LastRank players to the same member.

## Background jobs (`jobs.go`)

Four bulk flows run server-side rather than as browser loops: `lastrank_extended`,
`nap_members`, `ext_alliance_gather`, `prospect_refresh_transfer` /
`prospect_refresh_prospect`. Progress is persisted to `background_jobs` /
`background_job_items` (migration 063), so any browser can watch a run — and the
run survives the tab closing.

### THE RULE — read → fetch → write

`database.go` sets `db.SetMaxOpenConns(1)`. A `Step` that holds a rows cursor or an
open transaction across its upstream HTTP call waits forever for a connection that
can never be freed, and it **hangs the whole process silently** — no error, no log,
no panic. Every `Step` must be shaped:

```
read what you need (short query, cursor closed)
  → upstream fetch (NO db handle held, no open tx)
    → db.Begin() → write → Commit()
```

`syncOneMember`, `refreshOneExternalAlliance` and `refreshOneProspect` are written
this way and shared by both the HTTP handler and the job. `TestRunnerDoesNotHoldTheConnectionDuringStep`
guards the runner half: it hangs (rather than failing cleanly) if that regresses.

### Adding a flow

Implement `jobRunner` (`Plan` / `Step` / `Finish`) in a `jobs_<feature>.go` and call
`registerJobKind` from that file's `init()` — registration lives next to the
implementation, so adding a flow never touches `jobs.go`. Then attach the UI with
`JobProgress.attach()` (`static/job-progress.js` + `job-progress.css`).

**Gating: `Permission` for a single key, `Allow` for anything else.** `resolveJobKind`
prefers `jobKind.Allow` when set. Use it whenever the flow's HTTP surface gates on a
*disjunction* — the external-alliance gather is writable by `manage_allies` **or**
`manage_vs_points` — and pass the same predicate the handler uses (`Allow:
canManageExternalAlliances`). A single string silently can't express that:
`userHasPermission` resolves an unknown key to `false` via
`COALESCE(json_extract(...), 0)`, so the button renders for both manage ranks and the start
403s for everyone but admins. That was a live bug until the `Allow` predicate landed — if
you find yourself inventing a permission string, check it is a real `RankPermissions` field
first.

- **`Plan` must fully drain any cursor before returning** — the runner writes
  immediately afterwards.
- **Returning zero items is a success, not an error.** No items → no `Finish`, so
  no activity row. Idle ticks must not write audit noise.
- **`Step` returning an error records that item as `err` and the run CONTINUES.**
  One unreachable player must never sink a hundred-item sweep.
- **Order oldest-touched-first** so an interrupted run resumes rather than
  restarting. Advance the ordering column even for skipped items, or the sweep
  retries the same row forever.
- **`Finish` writes ONE summary activity row** for the whole run, not one per item.

### Operational notes

- **One job at a time, process-wide.** Every flow shares the same 1 req/sec upstream
  limiter, so a second concurrent run would only halve both rates while doubling
  contention on the single connection. A second start returns **409** naming the
  running kind and how long it has been going.
- **Restart handling**: `reconcileInterruptedJobs()` at boot marks any row still
  `running` as `interrupted` — that process is gone. No resume machinery exists or
  is needed, because re-running *is* resuming.
- **Shutdown**: `drainJobs` cancels and waits, bounded. An in-flight enrich has its
  own 25s ceiling and can outlast the drain; the boot-time reconcile is the backstop.
- **Retention**: `pruneOldJobs` keeps the newest 10 runs per kind and deletes item
  rows **explicitly** — `foreign_keys` is off app-wide so the schema's
  `ON DELETE CASCADE` never fires.
- **Scheduled runs** use `jobActor{UserID: 0, Scheduled: true}`; `logActivity` maps a
  non-positive id to `NULL` rather than a dangling `users(id)` reference.

## Known gotchas

### One DB connection — a query issued while a cursor is open DEADLOCKS

`database.go` sets `db.SetMaxOpenConns(1)`. `db.Query` holds that single connection until
`rows.Close()`, so **any** `db.Query` / `db.QueryRow` / `db.Exec` issued while a rows cursor is
still open waits forever for a connection that will never be free. This hangs the whole process,
not just the request — and it is silent: no error, no log, no panic.

```go
// WRONG — deadlocks the server
rows, _ := db.Query(`SELECT ... FROM external_alliances`)
defer rows.Close()
cfg := loadSomeConfig()          // <-- db.QueryRow while rows is open. Hangs forever.
for rows.Next() { ... }

// CORRECT — read everything you need BEFORE opening the cursor
cfg := loadSomeConfig()
rows, _ := db.Query(`SELECT ... FROM external_alliances`)
defer rows.Close()
for rows.Next() { ... }
```

The same applies inside a transaction: read on `tx` (never `db`), and `rows.Close()` before the
next statement on that `tx`. When a handler needs both a network call and a transaction, do the
**entire** network call before `db.Begin()` — never hold the connection across the wire.

Shape any read-then-write handler as **read-all-into-memory → close cursor → write-all**. See
`refreshNAP`/`applyNAPLadder` in `handlers_nap.go` and the note at `handlers_polls.go:854`.

### Timestamps: compute in SQL, and never assume the shape you read back

Two separate traps, both silent.

**Writing — never bind a Go `time.Time` to a timestamp column.** `database.go` opens the
DB with a bare path (no `_time_format` in the DSN), so the driver formats a bound
`time.Time` with `time.Time.String()`:

```
2026-08-04 12:34:56.789012345 +0200 CEST     ← what lands in the column
2026-08-02 10:34:56                          ← what CURRENT_TIMESTAMP writes (UTC)
```

Both are TEXT, so `expires_at > CURRENT_TIMESTAMP` is a **lexical string comparison**
between a local wall-clock value carrying an offset and zone name and a UTC value with
neither — the effective TTL silently shifts by the host's UTC offset. This bit
`invite_tokens` (a 48h invite expired after ~37h under `TZ=Pacific/Midway`); it was
invisible in production only because nothing sets `TZ`, so containers default to UTC.

Compute the value in SQL instead — same shape, same basis, no host timezone in the path:

```sql
INSERT INTO password_reset_tokens (..., expires_at) VALUES (..., datetime('now', '+24 hours'))
```

When Go genuinely must supply the value, format with `sqliteTimeLayout`
(`lastrank_client.go:405`) — never `time.RFC3339`.

**Reading — a column DECLARED `TIMESTAMP`/`DATETIME`/`DATE` does not come back as you
wrote it.** The driver parses those declared types into a `time.Time`, and `database/sql`
then renders that into a string destination as **RFC3339Nano**. So a column written by
`CURRENT_TIMESTAMP` as `2026-08-02 23:24:33` scans into a `string`/`sql.NullString` as
`2026-08-02T23:24:33Z`. Parsing it with a single space-form layout silently never
matches. This turned the mobile token-revocation check into a no-op that allowed every
token until it was caught in end-to-end testing.

Parse with `lastRankParseTime`, which handles both shapes, and compare as `time.Time` —
never as strings.

### CSP — no inline scripts allowed (`script-src 'self'` only)
`install.sh` sets `script-src 'self' https://cdn.jsdelivr.net` — **`'unsafe-inline'` is not in `script-src`**. Any inline `<script>` block in a template will be silently blocked in production (and on Android, this is immediately visible as a broken feature).

All template config vars have been migrated to `data-*` attributes. **Never add a bare `<script>` block to a template.** Use `data-*` on a container element instead:

```html
<!-- After (in template) -->
{{define "scripts"}}
<div id="page-config" data-can-manage="{{if .CanManage}}true{{else}}false{{end}}" hidden></div>
<script src="/feature.js"></script>
{{end}}
```

```javascript
// After (in JS)
const cfg = document.getElementById('page-config').dataset;
const CAN_MANAGE = cfg.canManage === 'true';
```

If you need layout.html-level JS (e.g. mobile nav handlers), add it to `static/global.js` — not as an inline script.

### XLSX export needs the SheetJS tag on that page template

CSV export is self-contained in `global.js`; **XLSX is not**. `exportTableToXLSX`
depends on SheetJS, which is a per-template CDN script:

```html
<script src="https://cdn.jsdelivr.net/npm/xlsx@0.18.5/dist/xlsx.mini.min.js"></script>
```

The trap is that the button is wired up whether or not the lib is there:
`global.js` auto-attaches **both** a CSV and an XLSX button to every
`table[data-export-csv]`. Miss the script tag and CSV works while XLSX throws a
`ReferenceError` — a dead button, no toast, nothing but a console line. The
Members page shipped that way.

So a page needs the tag if **either** is true:
- it has a `data-export-csv` table (the buttons are auto-wired), or
- its JS calls `exportTableToXLSX` / `XLSX.*` directly (e.g. `polls.js`, whose
  tag lives in `comms.html` — the template that loads it, which is not always
  the one named after it).

`build-check.yml`'s **"XLSX export dependency check"** fails the PR on both
cases. `exportTableToXLSX` also guards on the lib being absent and shows a toast,
so a miss degrades visibly rather than silently — but the check is what stops it
reaching a user.

### Browser translation — never build a payload from rendered DOM text

The app has no i18n framework; non-English speakers use the browser's built-in
translation (Chrome/Edge/Safari). That rewrites **text nodes in place**, so any
value read back out of the DOM is whatever the translator last put there.

The corollary is that **attributes are safe** — the translator never rewrites
them. Quick Search relies on exactly this, matching on `row.dataset.search`
rather than `textContent`; see the comment above `QuickSearch` in
`static/global.js` and rule 2 of "Searchable lists" below. Same rule, two
readings: what the translator can reach is text nodes, not attributes.

```javascript
// Wrong — copies/saves/exports the browser's translation
element.textContent = generatedText;
// ...later...
save(element.textContent);

// Correct — the rendered node is display-only
let sourceText = generatedText;
element.textContent = sourceText;
// ...later...
save(sourceText);
```

Retain the original value in a variable (or re-read the data object that
produced it) and build every clipboard copy, save body, and export from there.
This is already the pattern in `copyWithVariables` (`mail.js`), which copies
from the fetched template `content` and from `input.value` — never from
rendered text. `storm.js`'s battle mail was the exception and now holds
`generatedMailText`.

Form controls are the mirror image of the same trap: **never render user
content into a form control server-side.**

```html
<!-- Wrong — a translatable text node; saving the form writes the translation back -->
<textarea>{{.Notes}}</textarea>

<!-- Correct — ship it empty and populate from JS -->
<textarea id="notes-field"></textarea>
```

```javascript
document.getElementById('notes-field').value = data.notes;
```

`.value` set from JS is not a text node and is not translated, so the round
trip is safe. Server-rendered `<textarea>{{...}}</textarea>` content is, and
overwriting the original is silent — no error, no log line. `build-check.yml`
fails the build on that pattern.

### `translate="no"` on identifier surfaces, not on prose

`translate="no"` is inherited by descendants and can be overridden by
`translate="yes"` further down, so mark a container once rather than every leaf.

Apply it to surfaces whose text is an **identifier or a payload** — player and
alliance names, tags, anything pasted into the game verbatim, and every table
the CSV/XLSX helper can reach. `_extractTableData` (`global.js`) reads
`th.textContent` / `td.textContent`, so a translated table exports translated
member names that no longer match on re-import. **Any new table given
`data-export-csv` must also carry `translate="no"`** — and so must any template
table with a name column (`Member`, `Name`, `Player`, `Commander`, `Conductor`,
`VIP`, `Alliance`, `User`, …), exportable or not: a translated player name is one
nobody can find in the game. `build-check.yml` enforces both, keyed on the `<th>`
text for the second.

Do **not** apply it to member-written prose — shout-out notes, mail bodies,
ally/prospect notes, strike reasons, reward notes. That content is exactly what
a non-English speaker needs translated. Where prose sits inside a marked table,
opt the cell back in (see the Note cell in `renderRewardsTable`, `season-hub.js`).

Two things that look like they need it but don't:

- **A table built detached from the document** — the translator never sees it.
  `buildExportTable` (`members.js`) constructs its export table at click time
  from the data array, so that export is already safe.
- **`<input>` / `<select>` values** — `input.value` and `option.value` are not
  translated. Only `option.text` is, which is why reading `.value` is correct
  and `storm.js`'s registration table reading `option.text` for its time-slot
  headers is fine (display text, not payload). The flip side still bites the
  *reader*: a `<select>` listing member names **renders** translated names, so a
  roster dropdown needs `translate="no"` on the control even though what it
  submits is safe.

#### Marking JS-built DOM: `noTranslate` (`global.js`)

Templates say this with a `translate="no"` attribute. JS-built DOM has no such
container to mark, so mark the node as it is built — three helpers, all in
`global.js` and therefore loaded before every page script:

| Helper | Use |
|---|---|
| `noTranslate(el)` | Stamp (and return) the element holding an identifier. |
| `setLabeledName(el, label, name)` | Render `"<prose label><identifier>"` — e.g. `setLabeledName(h2, 'Archive ', name)`. Keeps the label translatable; a plain `el.textContent = 'Archive ' + name` freezes both or neither. |
| `noTranslateChoices(instance)` | Wrap every `new Choices(…)` over a list of names. **Choices.js wraps the original `<select>` and renders the visible list as a SIBLING of it**, so `translate="no"` on the `<select>` never reaches the rendered items. |

The `el()` helpers in `dashboard.js` / `lastrank.js` / `vs-league.js` /
`external-alliances.js` / `alliance-report.js` pass unknown keys through to
`setAttribute`, so those files write `translate: 'no'` in the props object
instead.

**Mark the smallest element that wraps the identifier.** Marking a whole card,
row or table also freezes its buttons, headings and empty states, which are UI
prose a non-English reader does need. Where a row genuinely mixes the two, split
it: a bare text node can't be opted out, so give the name a `<span>` of its own
(see `buildMemberChip` in `storm.js` and `defaultRenderRow` in `member-picker.js`).

A leaf already inside a marked ancestor needs nothing — the rows built by
`rankings.js`, `vs.js`, `upload.js` and `season-hub.js` land in tables the
template already marked.

### Inline translation of member prose: `TranslateBlock`

Page translation is all-or-nothing and can only run in one direction. It does
nothing for the opposite case — an English reader meeting one Spanish shout-out
inside an English page — because `layout.html` declares `lang="en"` and Chrome
detects the page as English, so the bar is never offered however foreign that
card is.

`TranslateBlock.attach(el, sourceText)` (`static/translate.js`, loaded from
`layout.html` after `global.js` so `svgIcon` exists) puts a per-block Translate
control on member-written prose. Always pass the text from the **data**, not
`el.textContent`. Currently wired to 8 render sites: shout-out notes
(`dyno.js`), season mail items (`season-hub.js`), prospect notes
(`recruiting.js`), ally notes (`allies.js`), schedule event notes
(`schedule.js`), and the strike/excuse reason cells (`accountability.js`,
`accountability_profile.js`).

**Three tiers, in this order** (`resolve()` in `translate.js`):

1. **Shared server cache** — keyed on `sha256(text) + target_lang`, so it needs
   **no language detection at all**. That is what makes it viable as the first tier.
2. **On-device, only when `Translator.availability()` is `'available'`** — never at
   `'downloadable'`, which is the path that stalls. Note Chrome **masks** pack status
   for anti-fingerprinting and reports `'downloadable'` regardless of what is cached,
   so this tier rarely fires there. It is honest, not load-bearing: **do not build
   anything that depends on it firing.** With a backend configured, the practical
   behaviour is server-wins.
3. **Server backend** (`translation.go`) — Cloud Translation auto-detects the source,
   so this tier needs no client detection either. That is what makes it work on a
   phone, where the built-in APIs do not exist.

The extra cache round trip is only spent when tier 2 is plausible; otherwise the
client goes straight to the full request, which checks the cache server-side anyway.
Common path: **one** round trip.

**Rules that must hold:**

- **On-device results are NEVER written to the shared cache.** Every user reads that
  store; letting a client write to it is a way to put words in another member's mouth.
  Server-authored only.
- **The cache is also the spend ledger.** `monthlyCharsUsed()` sums `char_count` for
  the month, so **rows are never DELETEd** — a delete undercounts the month and the cap
  leaks. Anything retiring an entry (a future "this translation is wrong" flag) must use
  a column, not a DELETE.
- **Same-language answers store an EMPTY `translated_text`**, with `source_lang ==
  target_lang` as the marker. Writing the original back would put a second copy of
  member prose in a table that deliberately holds only a hash of it.
- **Hashing happens server-side.** The obvious alternative — the client hashing and
  asking about a digest — needs `crypto.subtle`, which exists only in a **secure
  context**. This app is routinely reached over plain HTTP on a LAN, which is exactly
  the deployment a server backend exists to serve.
- **`/api/translate` refuses unless the mode is a server mode.** Without that, an
  install holding `gcp_vision` credentials for OCR could have its Translation quota
  spent on a feature the operator never enabled. It is also the only route in the app
  that spends a metered external quota, hence the per-user rate limit.
- **`Translate()` is shaped read → fetch → write** — cache read and budget SUM complete
  before the upstream call, the INSERT after. With `SetMaxOpenConns(1)` anything else is
  the silent process-hang gotcha.

**Cloud setup** reuses the existing `gcp_vision` service-account credential — no second
key. The operator enables the Cloud Translation API and grants
`roles/cloudtranslate.user`; the project id is parsed from the key itself. `mimeType` is
set to `text/plain`, or the v3 default (HTML) mangles `&` and `<` in ordinary prose.

Three things to know before extending it:

1. **It is desktop-only, by nature of the API.** Chrome/Edge's built-in
   `Translator` / `LanguageDetector` do not exist on mobile browsers and need a
   secure context, so the control never renders on a phone or over plain-HTTP
   LAN access — absent, not broken. Do not "fix" this with a polyfill; making it
   work on mobile means a server-side fallback, which is a cost and privacy
   decision, not a coding one.

   > **The two engines disagree on `downloadprogress`.** Chrome reports `loaded`
   > as a 0–1 fraction; Edge reports `loaded`/`total` as byte counts. Normalise
   > via `progressPct()` — multiplying Edge's byte count by 100 renders
   > "Downloading 4823700%". Never branch on user agent here: the API is a
   > standards-track proposal, so treat it as a capability and tolerate both
   > shapes.
   >
   > **A first-run model download sometimes never starts**, leaving `create()`
   > pending with zero progress events. Microsoft documents the remedy as
   > restarting the browser. That is why the stall guard exists and why its
   > message invites a retry instead of reporting permanent failure.

   > **Testing it locally: use `localhost`, not the LAN IP.** `localhost` and
   > `127.0.0.1` are secure contexts without TLS, so `http://localhost:8080`
   > shows the control. `http://10.7.1.16:8080` — the same dev server reached by
   > IP, which is a normal way this app gets opened — is *not*, so the control
   > is silently absent and the feature reads as broken. Production behind Caddy
   > is HTTPS and fine.
2. **`attach` restructures the element.** The prose moves into a
   `<span class="tl-text">` so the control survives swapping text back and forth,
   and the element is stamped `data-export-text` with the **original**.
   `_extractTableData` (`global.js`) prefers that attribute, so a translated cell
   still exports its source text — without it, this feature would reintroduce
   precisely the export bug the rules above exist to prevent.
3. **Detection is deliberately two-track.** A model download needs user
   activation, which a render pass does not have. So the detector is created at
   render time only when it needs no download (giving the precise behaviour: no
   control at all on prose already in the reader's language); otherwise the
   control is shown and the language settled on first click, which does carry
   activation.

### One structural width breakpoint — everything else is fluid

The site has a **single** width breakpoint: `@media (max-width: 768px)` (and its
complement `@media (min-width: 769px)`). It exists for one structural reason — it
swaps the whole navigation chrome: desktop sidebar ↔ mobile header + fixed
bottom-tabs + off-canvas menu (`.app-shell` goes column, `.sidebar` hidden). That
discrete component swap can't be expressed fluidly, so it stays.

**Do not add new width breakpoints.** All other responsiveness is fluid:
- **Type & spacing** → `clamp(min, vw-based, max)` (e.g. `main { padding: clamp(15px, 3vw, 30px) }`).
- **Card/column grids** → intrinsic `columns: <width>` or `grid-template-columns: repeat(auto-fit, minmax(<min>, 1fr))` so they reflow on their own (see `#dashboard-grid`). For a *fixed set* of column counts, use **container queries** (`container-type: inline-size` + `@container`), which are component-scoped — not page breakpoints (see `.week-grid`'s 7/4/2/1 stepping).
- **Toolbars / button rows / headers** → `flex-wrap` + `margin-left:auto` / `justify-content` (see `.season-header`).
- **Overflowing tabs / wide tables** → `overflow-x: auto` (tab strip scrolls at all widths; tables use `.table-scroll`).

A genuine *mobile-shell* behaviour (something that only makes sense because the
bottom-tabs / off-canvas nav exists — e.g. the `--table-vh-buffer`, the mobile
sticky first table column, capping a drag pool's height) belongs **inside the
existing `≤768px` block**, not in a new breakpoint. Avoid coupling JS to a width
via `matchMedia`; prefer CSS. If you must, key it to `768px` so CSS and JS agree.

### Season reward tiers are per-season rows, and `reward_tier` has no CHECK

Reward tiers were four fixed columns on `seasons` (`tier_count_leader`/`core`/
`elite`/`valued`) plus a `CHECK(reward_tier IN (...))` on `season_rewards`.
Migration `068` replaced both with `season_reward_tiers` — a per-season list
keyed `(season_id, key)`, modelled on `season_trackables`.

**Dropping the CHECK required rebuilding `season_rewards`** (SQLite cannot ALTER
a CHECK away — same create-copy-drop-rename as `030_season_mail_text.sql`). That
CHECK was the *only* validation on `reward_tier`, so it is now enforced in Go by
`rewardTierExists`, called from **both** `handleRewardSave` and
`handleRewardUpdate`. Any new path that writes `season_rewards.reward_tier` must
call it too — without it the column accepts arbitrary strings.

Three more rules:

1. **A tier's `key` is immutable; its `label` is not.** The key is the value
   stored on every `season_rewards` row. Renaming the label deliberately flows
   through to rewards already assigned — safe because the tier list is
   per-season, so past seasons keep their own rows.
2. **A tier with rewards assigned cannot be deleted** (409), exactly as a
   trackable with recorded data cannot. Renders of an orphaned key still fall
   back to showing the raw key rather than blanking.
3. **Tier CRUD is gated on `manage_season_rewards` (R5), not
   `manage_season_hub`.** Tier counts were previously edited inside the R5-only
   Edit Season modal; reusing the trackables gate would silently widen access to
   R4.

The global default list lives in `settings.season_reward_tiers_default` (mirroring
`season_score_levels_default`) and is copied into a season at creation.
**Do not move it into `season_templates.defaults`** — `buildDefaultsEditor`'s
`getJSON()` (`static/settings.js`) re-emits a fixed three-key object, so anything
else in that blob is silently destroyed on the next template save.

Badge colours are palette slots (`purple`/`info`/`success`/`warning`/`danger`/
`neutral`) validated against `validTierColors` in Go and rendered as
`.tier-badge.tone-*` in **`styles.css`** (global — the Settings page previews
them and does not load `season-hub.css`). There are exactly **two** lists to keep
in sync: `validTierColors` (Go) and `PALETTE_COLORS` (`color-picker.js`).

**Colour pickers live in `color-picker.js`, NOT `global.js`.** The `ColorPicker`
namespace (`PALETTE` / `buildSelect` / `make` / `setValue` / `upgradeAll` /
`destroyIn`) drives both the file-tag editor and the two reward-tier editors, with
its preview CSS in `color-picker.css`. Pass `'tier-color-choices'` as `extraClass`
to render each option as the badge it produces instead of a swatch dot.

> **Why not `global.js`:** the upgrade needs Choices.js, a per-template CDN
> script, while `global.js` loads on every page — so a helper there implies an
> availability it cannot promise. `exportTableToXLSX` is the cautionary tale in
> the other direction (see "XLSX export needs the SheetJS tag"): it sits in
> `global.js`, depends on the per-template SheetJS tag, and shipped as a dead
> button on the Members page. Keep a CDN-dependent helper beside its dependency.
>
> `build-check.yml`'s **"Colour picker dependency check"** enforces it, failing a
> template that loads `color-picker.js` without Choices or without
> `color-picker.css`, and one whose page JS calls `ColorPicker.*` without loading
> the module at all.

Every entry point degrades to a plain, working `<select>` when Choices is absent —
a missed tag costs the preview, never the feature. **Keep the Choices-dependent
half separate from anything that isn't**: an early `if (!window.Choices) return`
that also guards unrelated work silently disables it. That exact bug existed here
in review — `refreshOrderButtons` sat behind the Choices guard, so a page without
the lib would have lost its reorder end-stops too.

> The badge variant has to out-specify `choices-theme.css`, which paints the
> highlighted row accent-on-white at specificity **0-4-0**. Left alone it renders
> every option's text white on its own pale background — unreadable, and it
> defeats the preview. The per-tone `is-highlighted[data-value]` rules restate the
> tone at 0-5-0 and turn hover into a ring instead of a fill. Any new
> `.color-choices` variant needs the same treatment.

**User-ordered lists use buttons, not a `sort_order` field.** `buildOrderButtons(tr,
onMove)` / `refreshOrderButtons(tbody)` / `rowPosition(tr)` (`global.js`) move a row
among its siblings and disable the direction it can't go. `rowPosition` is
deliberately not `tr.rowIndex`, which counts header rows. In the per-season editor a
move persists immediately (one PUT per row whose position actually changed) so the
badges behind the modal stay in step; unsaved new rows are skipped and pick up their
position when first saved.

### Former/archived members have rank `'EX'`, not `'Former'`
Members removed from the alliance are stored with `rank = 'EX'`. Any query or JS filter that needs only active members must exclude this rank explicitly. Filtering by `'Former'` silently does nothing — that string does not exist in the data.

```sql
-- Wrong — 'Former' never matches anything
WHERE m.rank != 'Former'

-- Correct
WHERE m.rank != 'EX'
```

```javascript
// Wrong
members.filter(m => m.rank !== 'Former')

// Correct
members.filter(m => m.rank !== 'EX')
```

### rank is TEXT, not INTEGER
`rank_permissions.rank` is `TEXT PRIMARY KEY` with values `'R1'`–`'R5'`. Integer comparisons silently do nothing.

```sql
-- Wrong
UPDATE rank_permissions SET col = true WHERE rank >= 4;

-- Correct (see 008_schedules.sql as the canonical pattern)
UPDATE rank_permissions SET col = 1 WHERE rank IN ('R4', 'R5');
```

### Modals must go in `{{define "modals"}}`, not `{{define "content"}}`
`layout.html` renders `{{block "modals" .}}` **outside** the `.container` div. Modals placed inside `{{define "content"}}` are trapped inside the container, which creates a stacking context that breaks `position: fixed` overlays — the backdrop won't cover the page correctly.

```html
{{define "content"}}
  <!-- page body only -->
{{end}}

{{define "modals"}}
  <div id="my-modal" class="modal">
    <div class="modal-content">...</div>
  </div>
{{end}}
```

### Use the global `.modal` / `.modal-content` classes
`styles.css` already defines `.modal` (hidden by default, toggled via `style.display='flex'`) and `.modal-content` (styled box). Don't write custom modal CSS — use these.

### Never add `class="hidden"` to a `.modal` element
`.modal` is already `display: none` by default. Adding `.hidden` (which is `display: none !important`) is redundant, but more importantly it **breaks the open logic**: `element.style.display = 'flex'` cannot override `!important` from a stylesheet rule, so the modal will silently stay hidden.

- **Open**: `modal.style.display = 'flex'`
- **Close**: `modal.style.display = ''` (clears the inline style; `.modal`'s own `display: none` takes back over)

```html
<!-- Correct — no extra hidden class needed -->
<div id="my-modal" class="modal">
```

### Tab switching: use `style.display = 'block'`, never `style.display = ''`
`styles.css` has a global rule `.tab-content { display: none }`. If you clear a tab's inline style with `style.display = ''`, the element reverts to the CSS rule and stays hidden — tabs appear broken (cursor changes, nothing happens).

Always show the active tab with an explicit value:
```javascript
// Wrong — reverts to CSS display:none
target.style.display = '';

// Correct
target.style.display = 'block';
```

Also show the **initial** active tab explicitly in `DOMContentLoaded` — CSS hides all `.tab-content` by default, so nothing is visible on load until JS sets it:
```javascript
const activeBtn = document.querySelector('.tab-btn.active');
if (activeBtn) {
    const target = document.getElementById('tab-' + activeBtn.dataset.tab);
    if (target) target.style.display = 'block';
}
```

`.tab-bar` / `.tab-btn` styles are defined in `styles.css` (globally available). No need to add them to page CSS — they work automatically on any page that uses `.tab-bar` / `.tab-btn` markup.

### Inline `style="display:none"` in templates is intentional (Category B)

Dozens of `style="display:none"` inline styles remain across `templates/`. These
were triaged in Epic 43 and confirmed as **Category B**: elements that JS reveals
later via `element.style.display = 'block'` (or `'flex'`). The inline style hides
them on first paint, before the page script runs — removing it would flash the
element visible on load.

Do **not** bulk-remove these. Before deleting any one of them, find the matching
JS toggle that shows it (`getElementById(...)...style.display = ...`) and confirm
it isn't the initial-hidden state for that element.
When a [Choices.js](https://cdn.jsdelivr.net/npm/choices.js@10.2) `<select>` lives inside a `<form>`, Choices attaches a `reset` listener to that form. On `form.reset()` it restores the dropdown to the state captured **at init** — i.e. only the `<option>`s present in the template HTML — silently discarding anything you added later via `setChoices()`.

This bit `admin.js`: the roster is loaded once at page-load (`populateMemberDropdown` → `setChoices`), but `showCreateUserModal()` calls `user-form.reset()`, which emptied the member dropdown every time the New User modal opened.

If you call `form.reset()` on a form containing a Choices select, **re-run your `setChoices()` populate immediately after the reset** (see `showCreateUserModal` in `admin.js`). Selecting a value with `setChoiceByValue()` is not enough — that sets the selection, it does not restore the option list.

### CSRF is handled globally
`static/csrf.js` intercepts all `fetch` calls and injects `X-CSRF-Token` on POST/PUT/DELETE automatically. You don't need to manually attach the token in page JS.

### Pass `canManage` to the template, not the permission column name
Handlers resolve the boolean server-side and pass it to the template. The column name never reaches the frontend.

```go
data := getPageData(r, "...", "feature")
data.CanManage = data.Permissions.ManageFeature
renderTemplate(w, r, "feature.html", data)
```

```html
{{define "scripts"}}
<script>window.CAN_MANAGE = {{if .CanManage}}true{{else}}false{{end}};</script>
<script src="/feature.js"></script>
{{end}}
```

### Parameterised queries everywhere
No string-formatted SQL for user input. All DB writes should use `?` placeholders.

### Wrap deletes in a transaction
Even when cascades handle children, wrap category/parent deletes in a transaction — consistent with existing handlers.

### Never use browser `alert()` or `confirm()`
`alert()` and `confirm()` block the main thread, look out of place, and vary wildly across browsers/OS. Do not add new calls to either.

**For success/error feedback** — show an inline status message near the triggering action (e.g. a `<p class="status-msg">` that you set `textContent` on and clear after a few seconds), or a non-blocking toast element.

**For destructive confirmations** — use the inline button-swap pattern: hide the Delete button, append a `"Sure?" [Yes] [No]` span in its place, and restore the original button if the user picks No. Never use `confirm()`. Example from `train.js`:

```javascript
delBtn.addEventListener('click', () => {
    delBtn.style.display = 'none';
    const confirmSpan = document.createElement('span');
    confirmSpan.style.cssText = 'display:inline-flex;gap:4px;align-items:center;';
    const label = document.createElement('span');
    label.textContent = 'Sure?';
    label.style.fontSize = '0.85rem';
    const yesBtn = document.createElement('button');
    yesBtn.className = 'btn btn-danger btn-sm';
    yesBtn.textContent = 'Yes';
    yesBtn.addEventListener('click', () => doDelete(item.id));
    const noBtn = document.createElement('button');
    noBtn.className = 'btn btn-secondary btn-sm';
    noBtn.textContent = 'No';
    noBtn.addEventListener('click', () => { confirmSpan.remove(); delBtn.style.display = ''; });
    confirmSpan.append(label, yesBtn, noBtn);
    actionsContainer.appendChild(confirmSpan);
});

Note: many existing files still use `alert()` — do not add more, and replace them when touching those files.

### Validate required CSV columns before the row loop — never silently skip
After building a `colMap` from CSV headers, check that all required columns are present **before** entering the row loop. A missing column causes every row to hit a `continue`, returning an empty result with no error — a silent failure that's very hard to debug.

```go
// Wrong — silently skips all rows if "name" column is missing
for _, row := range records[1:] {
    nameIdx, ok := colMap["name"]
    if !ok { continue }
    ...
}

// Correct — fail fast with a clear error
nameIdx, ok := colMap["name"]
if !ok {
    http.Error(w, "CSV missing required column: Member (or Name)", http.StatusBadRequest)
    return
}
for _, row := range records[1:] { ... }
```

Also map expected header aliases up front (e.g. `"member"` → `"name"`, `"day 1"` → `"monday"`) so the column check is reliable regardless of export format.

### Never leak raw errors to the client
Do not pass `err.Error()` (or any internal error string) directly to `http.Error`. Log the detail server-side with `slog.Error` and return a generic message to the client.

```go
// Wrong
http.Error(w, err.Error(), http.StatusInternalServerError)

// Correct
slog.Error("short description of what failed", "error", err)
http.Error(w, "Database error", http.StatusInternalServerError)
```

For bad-request (400) errors from JSON decode failures, use `"Invalid request body"` — no logging needed since it's a client error. Validation messages (missing fields, bad enum values) are safe to return as-is since they are written by us, not sourced from the DB or runtime.

## Template blocks

| Block | Purpose |
|-------|---------|
| `header_text` | `<h1>emoji Title</h1><h2>Subtitle</h2>` — matches page header style |
| `head_tags` | Link page CSS: `<link rel="stylesheet" href="{{asset "/feature.css"}}">` |
| `content` | Main page body |
| `scripts` | JS includes + inline `window.*` config vars |
| `modals` | Modal HTML — rendered outside the container div |

## Adding a new permission

Permissions are stored as a JSON blob in the `rank_permissions.permissions`
column (one row per rank), serialized from the `RankPermissions` struct — **not**
as one column per permission. Since Epic 30 there is no migration and no
SELECT/Scan change; adding a permission is two edits in `models.go`:

1. Add `FieldName bool \`json:"key_name"\`` to the `RankPermissions` struct.
2. Add a `PermissionRow{Key: "key_name", Label: "Human Label"}` to the
   appropriate `PermissionGroup` in `PermissionGroups` (or add a new group).

No migration required — the new key round-trips through `json.Marshal` /
`json.Unmarshal` automatically and appears in the Settings → Permissions Matrix
UI. `allPermissionsTrue()` (admin shortcut) reflects it via struct reflection,
so nothing else needs touching.

> Note: legacy per-rank permission *columns* added by `008_schedules.sql` and
> friends predate the JSON blob and are no longer the write path — do not add
> new ones.

## Frontend JS hardening

All JS files are being migrated away from `innerHTML` string injection to safe DOM construction. Work is tracked on the `js-hardening` branch, one file per session.

**Target pattern** — use `createElement` + `textContent`, never build HTML strings:
```javascript
// Safe
const card = document.createElement('div');
card.className = 'member-card';
card.textContent = member.name;   // never executes HTML
container.appendChild(card);

// For clearing + replacing children
container.replaceChildren(...items);  // or replaceChildren(singleNode)
```

**Event handling** — wire via `addEventListener`, never inline `onclick` in JS-generated markup:
```javascript
btn.addEventListener('click', () => editMember(member.id));
```

**`escapeHtml()`** — remove at the injection point when converting to `textContent`. Do not leave orphaned calls.

**Modal open/close check** — the global `.modal` class uses `display: flex` for centering. Always open modals with `modal.style.display = 'flex'` (never `'block'`) and close with `modal.style.display = ''`. Never add `class="hidden"` to a `.modal` element — see gotcha above. Verify open/close on every file during hardening.

**Progress**:
| File | Status |
|------|--------|
| `static/members.js` | ✅ Done |
| `static/storm.js` | ✅ Done |
| `static/rankings.js` | ✅ Done |
| `static/vs.js` | ✅ Done |
| `static/dyno.js` | ✅ Done |
| `static/admin.js` | ✅ Done |
| `static/settings.js` | ✅ Done |
| `static/profile.js` | ✅ Done |
| `static/schedule.js` | ✅ Done |
| `static/upload.js` | ✅ Done |
| `static/files.js` | ✅ Done |
| `static/recruiting.js` | ✅ Done (written with safe patterns from the start) |
| `static/login.js` | ✅ Done (same password-rules pattern as profile.js; fixed during final sweep) |
| `static/invite.js` | ✅ Done (same password-rules pattern as login.js) |
| `static/accountability.js` | ✅ Done (uses safe DOM patterns throughout) |
| `static/accountability_profile.js` | ✅ Done |
| `static/accountability_report.js` | ✅ Done |
| `static/activity.js` | ✅ Done |
| `static/allies.js` | ✅ Done |
| `static/comms.js` | ✅ Done |
| `static/polls.js` | ✅ Done (written with safe patterns from the start) |
| `static/dashboard.js` | ✅ Done (reference implementation of the `el()` helper pattern) |
| `static/season-hub.js` | ✅ Done |
| `static/train.js` | ✅ Done |
| `static/mail.js` | ✅ Done (2 `.onclick` property assignments on global modal buttons — safe, not markup injection) |
| `static/officer_command.js` | ✅ Done (skip — already used correct patterns) |
| `static/global.js` | ✅ Done (utility — no DOM injection) |
| `static/theme.js` | ✅ Done (utility — no DOM injection) |
| `static/csrf.js` | ✅ Done (utility — no DOM injection) |
| `static/modal-focus.js` | ✅ Done (utility — keyboard focus trap only) |

## JS DOM standards — colors and styles

The same token rules that apply to CSS apply to JavaScript. Every color set via `element.style.cssText` or `element.style.color` must use `var(--token)`. Hardcoded hex values do not respond to theme changes.

```javascript
// ❌ Wrong — hardcoded, breaks dark mode
span.style.cssText = 'color:#63b3ed;font-size:0.85em;';

// ❌ Wrong — non-existent token, silently falls back
span.style.color = 'var(--danger-color)';

// ✅ Correct
span.style.cssText = 'color:var(--color-info);font-size:0.85em;';
span.style.color = 'var(--color-danger)';
```

When JS creates the same styled element repeatedly (e.g. a badge), the color belongs in a CSS class — not a `style.cssText` string duplicated across every render call. Apply classes via `element.classList.add('badge-hq')`.

**Chart.js** — read the color token at init time rather than hardcoding:

```javascript
// ❌ Wrong
Chart.defaults.color = '#718096';

// ✅ Correct
Chart.defaults.color = getComputedStyle(document.documentElement)
    .getPropertyValue('--text-muted').trim();
```

### Reference: `dashboard.js` `el()` helper

`dashboard.js` defines a clean DOM builder that never uses `innerHTML` or hardcoded colors. Use it as a reference pattern for any new JS file that constructs significant DOM:

```javascript
function el(tag, props, ...children) {
    const node = document.createElement(tag);
    if (props) {
        Object.entries(props).forEach(([k, v]) => {
            if (k === 'className')    node.className = v;
            else if (k === 'textContent') node.textContent = v;
            else if (k === 'style')   Object.assign(node.style, v);
            else node.setAttribute(k, v);
        });
    }
    children.forEach(c => {
        if (c == null) return;
        node.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
    });
    return node;
}
```

## UI feedback — never use browser dialogs

All user-facing feedback must go through the helpers in `static/global.js`. Browser-native `alert()`, `confirm()`, and `prompt()` are banned — they block the thread, ignore theming, and break automated tests.

| Need | Use |
|------|-----|
| Success/error notification | `showToast(message, type, duration)` — `type` is `'success'` (default), `'error'`, or `'info'` |
| Destructive confirmation | `await showConfirm(message, confirmLabel, title)` — returns `true`/`false` |
| Inline field validation | `setFieldError(fieldEl, message)` / `clearFieldError(fieldEl)` / `clearAllFieldErrors(formEl)` |
| Input from user | Add a dedicated `<div class="modal">` in `{{define "modals"}}` with its own form |

```javascript
// Confirmation before delete
if (!await showConfirm('Delete this entry?', 'Delete')) return;

// Toast on success
showToast('Entry deleted.');
showToast('Something went wrong.', 'error');

// Inline field error
setFieldError(document.getElementById('name-input'), 'Name is required.');
```

`showConfirm` supports a `title` parameter (third arg) for cases where the heading should differ from the body, e.g. displaying newly-created credentials.

## CSS variables — always use tokens, never hardcode colors

`styles.css` defines a semantic token layer in all four theme blocks (`:root`, `html.theme-light`, `html.theme-dark`, `@media (prefers-color-scheme: dark) html.theme-auto`). New feature CSS files must use these tokens exclusively — hardcoded hex/rgb values do not adapt to dark mode.

### Available tokens

| Token | Light value | Dark value | Usage |
|-------|-------------|------------|-------|
| `--bg-primary` | `white` | `#1a1f2e` | Main page / card backgrounds. Prefer over `--container-bg` in new code. |
| `--bg-secondary` | `#f8f9fa` | `rgba(255,255,255,0.05)` | Toolbars, filter bars, form sections, table row hover. |
| `--bg-tertiary` | `#edf2f7` | `rgba(255,255,255,0.08)` | Nested backgrounds, code snippets, non-primary table headers. |
| `--text-primary` | `#333` | `#e9ecef` | Body text, headings, labels. |
| `--text-secondary` | `#666` | `rgba(255,255,255,0.7)` | Subtext, metadata, table column headers. |
| `--text-muted` | `#6c757d` | `rgba(255,255,255,0.6)` | Placeholders, hints, timestamps. |
| `--border-color` | `#e9ecef` | `rgba(255,255,255,0.1)` | All borders, dividers, table separators. |
| `--color-primary` | `#667eea` | `#667eea` | Accent, focus rings, active states. |
| `--color-info-bg` / `--color-info` | `#dbeafe` / `#1d4ed8` | `rgba(59,130,246,0.15)` / `#93c5fd` | Info panels, at-risk badges. |
| `--color-success-bg` / `--color-success` | `#dcfce7` / `#15803d` | `rgba(34,197,94,0.15)` / `#86efac` | Success states, eligible badges. |
| `--color-warning-bg` / `--color-warning` | `#fef3c7` / `#d97706` | `rgba(251,191,36,0.12)` / `#fbbf24` | Warnings, needs-improvement badges, profile expiry notices. |
| `--color-danger-bg` / `--color-danger` | `#fee2e2` / `#dc2626` | `rgba(248,113,113,0.12)` / `#f87171` | Errors, destructive actions. |
| `--color-purple-bg` / `--color-purple` | `#ede9fe` / `#6d28d9` | `rgba(109,40,217,0.15)` / `#c4b5fd` | Role/privilege indicators, Admin nav, profession badges. |
| `--input-bg` / `--input-border` | `white` / `#dee2e6` | `#252b3b` / `rgba(255,255,255,0.2)` | Form inputs, selects, textareas. |

### Non-existent token names — do not use

These names appear in older code but are NOT defined in `styles.css`. They silently fall through to browser defaults in dark mode:

- `--danger-color` → use `--color-danger`
- `--accent-color` → use `--color-primary`
- `--primary-color` → use `--color-primary`
- `--text-color` → use `--text-primary`

### Additional CSS rules

- **New page CSS** goes in `static/feature.css`, linked via `{{define "head_tags"}}`. Never embed `<style>` in templates.
- **Prefer `--bg-primary`** over `--container-bg` in new page CSS files. `--container-bg` remains in the core layout rules where it already exists.
- **Buttons**: use `.btn .btn-primary / .btn-secondary / .btn-danger` (+ `.btn-sm`). The older `.primary-action-btn` / `.secondary-action-btn` classes are deprecated — do not use in new code.
- **Card borders**: interactive data-entity cards (member, award, day, mail item) use `border: 2px solid var(--border-color)`. Toolbar/filter panel cards use `border: 1px solid var(--border-color)`.
- **Global utilities** — the following classes are defined in `styles.css` and available on every page without importing page CSS:
  - `.data-table` — standard table with gradient header
  - `.filter-chip` / `.filter-chip-label` — pill-shaped filter button with active state
  - `.tab-toolbar` / `.status-msg` — tab action row and async status text
  - `.badge-hq` / `.badge-troop` / `.badge-profession` / `.badge-squad-type` — secondary member badges (used alongside `.member-rank`)
  - `.btn`, `.btn-primary`, `.btn-secondary`, `.btn-danger`, `.btn-sm`
  - `.form-input` — input/select/textarea styling (alias for `.form-group input`)
  - `.finder-*` — dropdown/head/item/name/meta/msg/err/action for the shared
    type-ahead combobox (`buildRemoteFinder` in `global.js`)
  - `.filter-search-wrap` / `.quick-search` — the search box + clear (×) button,
    used both inside the filter panel and standalone (`QuickSearch` in `global.js`)
  - `.export-scope` — the "Filtered only" checkbox injected beside the export buttons
  - `.tab-count` — row-count text beside a toolbar search ("12 of 98")

### Searchable lists: use `QuickSearch`

`QuickSearch` (`static/global.js`) is the shared list filter. Every member list in
the app goes through it, and **it folds diacritics** — a roster `Pàcha` matches a
typed `Pacha`, the same equivalence `foldName` (`namematch.go`) enforces server-side.
Hand-rolling `.toLowerCase().includes(...)` reintroduces the mismatch and **fails the
build** (`build-check.yml` → "Accent-folded search check").

```js
// Hide mode (default) — the row set is stable; only visibility changes.
QuickSearch.attach({
    input: 'thing-search', container: 'thing-tbody', rows: 'tr',
    emptyText: 'No members match your search.',
});
// ...and at the END of the render fn, so the filter survives a re-render:
QuickSearch.apply('thing-search');
```

Four rules:

1. **A list with editable cells or DOM-held state MUST use hide mode.** Re-rendering
   on every keystroke throws away unsaved input. `renderManualTable` (`season-hub.js`)
   did exactly that: typing in the Contributions search blanked every `.contrib-input`,
   re-fetched the values one request per keystroke, and dropped the hidden members
   from the save payload. Hiding also means the save loops — which walk rows, not the
   data array — still submit everyone while a filter is active.
2. **Stamp `row.dataset.search` from the SOURCE data**, not the rendered text.
   An attribute is never rewritten by the browser's translator, and whole-row
   `textContent` includes rank chips and button labels, so `e` would match every row
   with an Edit button. Store the raw value; folding happens at match time.
3. **Call `QuickSearch.apply('<input-id>')` at the end of the render fn.** Any
   re-render (week change, status filter, save, reload) drops the filter otherwise.
   `apply` looks the handle up off the input, so the render fn needs no reference to it.
4. **One search per list — never a quick search *and* the filter panel.** The panel
   already bundles its own box. Pages using `FilterPanel` (Members roster, Train logs,
   Files, External Alliances, Scout Report) must not gain a second one.

For pages that legitimately rebuild from their data array, `QuickSearch.match(text, q)`,
`.matcher(q)` and `.filter(list, q, pick)` are the folded predicates. `QuickSearch.widget()`
builds the standard input + clear button for toolbars constructed in JS.

**Export scope**: `_extractTableData` skips rows QuickSearch has hidden, so a
`data-export-csv` table exports what the officer can see. A "Filtered only" checkbox
appears beside the export buttons *only* while a filter is narrowing that table, and
the button labels carry the row count. This is automatic for hide mode; a table
filtered by re-rendering has no hidden rows to restore and would need to register a
full-data provider.

### Type-ahead pickers: use `buildRemoteFinder`

`buildRemoteFinder(opts)` in `static/global.js` is the shared combobox: instant local
matches as you type, plus a remote lookup behind an **explicit click**. Used by the
Recruiting player picker.

**Never fire the remote search per keystroke, debounced or otherwise.** Every remote
source is the volunteer-run LastRank API behind the shared 1 req/sec limiter, so
searching has to stay a deliberate act — one officer typing a name must not become a
burst of upstream calls.

> The older `.ext-find-*` (`external-alliances.css`) and `.vsl-find-*`
> (`vs-league.css`) pickers predate this helper and still hand-roll the same
> mechanics; migrating them is a pending follow-up — but only their mechanics.
> Their **tokens are already correct**: they use `--color-text-muted`, which is
> canonical and defined in `:root` and both `[data-theme]` blocks. An earlier
> version of this note claimed that token was undefined and told you to "fix" it
> to `--text-muted` / `--text-primary` / `--bg-primary`; that is backwards — those
> are the deprecated names (DESIGN_STANDARD.md → Legacy tokens). Migrate *to* the
> `--color-*` names, never away from them.

## Session Key Requirement

`SESSION_KEY` must be set in production. If unset, the app generates an
ephemeral key and logs a warning — this causes all users to be logged out
on every restart. A key shorter than `MinSessionKeyLen` (32 chars) already
refuses to boot (`os.Exit(1)`), but an **unset** key still boots with an
ephemeral one even in production mode. A future improvement should make the
app refuse to start in production (`PRODUCTION=true`) without a valid
`SESSION_KEY`.

**Operator action:** Confirm `SESSION_KEY` is set in all production deployments
before enabling `PRODUCTION=true`.

## Known technical debt

- `handlers_season_hub.go` `handleSeasonArchive` derives the archived season's
  `end_date` from `time.Now().UTC()`, not game-time (UTC−2). It was left out of
  the game-time clock consolidation (season *create* uses `gameDate()`); revisit
  in a future pass. Marked with a `TODO(game-time)` at the call site.

## Documentation

Keep `README.md` up to date whenever a user-facing feature is added, changed, or removed. Each feature should have an entry under the appropriate `###` section in the Features block, written in the same style as existing entries (bullet points, bolded lead phrase, plain-English description of what it does and its permission model). Do not document internal implementation details — README is for end users and operators.

### `docs/` — design references and API specs

Non-runtime reference material lives in `docs/`, not in `static/`. **Never put a
design mockup or reference document in `static/`**: the catch-all handler in
`main.go` sits on the bare router with no auth middleware, so anything in there is
served unauthenticated to anyone who guesses the URL, and `buildAssetHashes()`
SHA-256s it at every boot for nothing.

Third-party API references (e.g. the LastRank API notes) are kept in `docs/` but
**gitignored** — they are not ours to publish and this repo is public. See
`docs/README.md`.

## Static asset cache busting

Every same-origin `.js` / `.css` reference in a template goes through the `{{asset}}`
template function, which appends a content-hash query token:

```html
<link rel="stylesheet" href="{{asset "/feature.css"}}">
<script src="{{asset "/feature.js"}}"></script>
<!-- renders as /feature.css?v=a1b2c3d4 -->
```

**Never write a bare `src="/foo.js"` or `href="/foo.css"`** — `build-check.yml`'s "Versioned
static asset check" fails the PR, and it also fails an `{{asset}}` naming a file that isn't in
`static/`.

The token is the first 8 hex of the file's SHA-256, built once at boot by `buildAssetHashes()`
(`assets.go`) into a map that is **written before any request-serving goroutine exists and
read-only thereafter** — that ordering is its only synchronisation, so a runtime rebuild
(SIGHUP, file watcher) would need an `atomic.Pointer` first.

Because the token follows the *bytes*, the URL changes if and only if the content changes: a
restart busts nothing, and a deploy that touches one file re-downloads one file. This is
deliberately not a single global token stamped at startup — a restart is not a new file
version, and a boot timestamp would re-download all 70 files on every crash-loop and
`docker compose restart`.

**New template parse sites must use `parseTemplates()` (`assets.go`), never
`template.ParseFiles`.** `html/template` rejects an unknown function at *parse* time, so a
site that misses the FuncMap 500s on its first request. `parseTemplates` names the template
after `filepath.Base(files[0])`, which is what keeps both `t.ExecuteTemplate(w, "layout.html",
data)` and the standalone pages' `t.Execute(w, data)` working.

**Rendered HTML is `Cache-Control: no-store`** via `noStoreHTML()`, called *before*
`w.WriteHeader` (a header set afterwards is silently dropped — that is why 403 and 404 go
through `renderTemplateStatus`). Without it a cached page would carry stale `?v=` tokens and
defeat the whole scheme.

**Failing to version something is safe, just slower.** The static handler grants
`max-age=31536000, immutable` only when the request's `?v=` matches the file's current hash;
anything else gets `no-cache` plus an `ETag`, i.e. a revalidation round-trip and a 304 — never
a stale file. Verifying the token is also what makes `immutable` safe to promise: a naive "any
`?v=` means immutable" would pin new bytes under an old token for a year mid-rollout.

**`icons.svg` is deliberately NOT versioned.** `svgIcon()` (`static/global.js`) builds its
`<use href="/icons.svg#icon-…">` in JS, so versioning the 266 template refs without also
plumbing a token into that function would make the two disagree and fetch the 27 KB sprite
twice per page. It rides the `no-cache` + `ETag` fallback instead: one ~200-byte 304 per page,
body never re-downloaded.

Nothing here needs a `Caddyfile`, `Dockerfile`, or CI build-arg change — Caddy only adds its
named security headers and passes `Cache-Control` / `ETag` through untouched.

## Icons

All icons are **Tabler Icons** (outline set), delivered as a sprite at `static/icons.svg`.
`static/icons.svg` holds only the icons added so far — it is **not** the set to choose from.
When a feature needs an icon, pick the semantically correct one from the **full** Tabler
library (~5,900 icons, <https://tabler.io/icons>) and add its `<symbol>` to the sprite in the
existing format; don't reuse an approximate icon just because it's already there. Reference by
`<use href="/icons.svg#icon-{slug}">` in templates or `svgIcon('{slug}')` in JS. Full details:
DESIGN_STANDARD.md → Icon System.

## Running locally

```bash
go run .
```

Migrations run automatically on startup via `initDB()`.

**Static asset caching:** in non-production (`PRODUCTION != "true"`) the server sends
`Cache-Control: no-store` for `static/` files (see `main.go`), so JS/CSS/SVG edits reload
without stale-cache issues. In production those files are content-hashed and cached
far-future instead — see "Static asset cache busting". Templates and `static/` are served from
disk (dev volume mounts), so `.html`/`.css`/`.js`/`.svg` edits show on refresh with no rebuild;
Go changes still need `docker compose up -d --build` (or restart `go run .`).

Because dev deliberately disables caching, **cache behaviour cannot be tested in dev**. To
exercise the real production headers locally, run with `PRODUCTION=true` against a scratch DB
on a spare port:

```bash
PORT=8099 PRODUCTION=true SESSION_KEY=<32+ chars> DATABASE_PATH=/tmp/scratch.db go run .
curl -sI localhost:8099/styles.css   # inspect Cache-Control / ETag
```
