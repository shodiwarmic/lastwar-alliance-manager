# The demo alliance

A complete, plausible, entirely **fictional** install — **Cinder Vale** `[CNDR]`, server 1894 —
for trying the app without real data: the README screenshots are taken from it, and the public
demo runs on it. Every name, number and document in it is generated.

## The generator

```bash
go run ./cmd/demo-seed --db /tmp/demo.db --uploads /tmp/demo-uploads
DATABASE_PATH=/tmp/demo.db STORAGE_PATH=/tmp/demo-uploads go run ./cmd/server
```

Run both from the repository root (the migrations, templates and static files are read from
there). `demo-seed` creates the database, applies the real migrations, and fills it; it refuses a
database that already has users. It prints the six accounts it created:

| Account | Is |
|---|---|
| `demo-admin` | an administrator linked to no member — sees every page |
| `demo-r5` … `demo-r1` | an account per rank, each linked to a member of that rank — the "own" views (profile, own VS) |

All six share one password: `--password`, or a random one, printed. `--worker-url` stores an OCR
service URL (and switches the OCR backend to local mode) — the demo points it at the fixtures
service. `--manifest` says where to write `manifest.json` (default: beside the database), the
list of accounts, files and canned leaderboards the fixtures service answers from.

**What it fills.** 100 member rows (1 R5, 9 R4, 55 R3, 15 R2, 6 R1, 14 former), aliases, twelve
weeks of power / hero / squad / kill / HQ / profession history, eight VS weeks, a four-week Duel
League season, Seasons 1–3 (Season 3 active, with weekly participation and contributions),
±6 weeks of schedule, three recorded event boards, strikes, train logs, a Desert Storm plan,
recruits, allies, polls, shout-outs, the Officers directory, four sample files (a `.docx`, an
`.xlsx`, a `.csv` and a `.png`), a few LastRank review decisions, and a month of activity.

**Dates.** Everything is dated relative to an anchor. By default the anchor is today's game date
(UTC−2), which is what the demo uses: each reset re-dates the alliance so it never looks
abandoned. `--today YYYY-MM-DD` fixes the anchor instead. The data is otherwise the same for every
anchor — same members, same numbers — only the dates move.

> **The server reads "now" from its own clock, not from the seed.** The current VS week, the
> schedule's "today", the Season Hub's current week and the dashboard all follow the real date.
> A database anchored on another day therefore shows those views as of the wrong week. Seed with
> the anchor you will run it on — today, by default.

**Where the numbers come from.** `internal/demo/params.go` holds the distributions the roster is
sampled from: coarse, rounded aggregates of a real alliance (deciles of power, kills and so on,
VS points per weekday), each beside the read-only query that produced it. No row, name or single
player's figure is in it — the top decile, which ends at one real player, is never measured.
Names are composed from syllable tables in several scripts; nothing is drawn from a list of
players.

## The fixtures service

The demo cannot run the OCR service or Collabora, so one small binary stands in for both:

```bash
go run ./cmd/demo-fixtures -port 9090 -frame-ancestors http://localhost:8080
```

| Route | Stands in for | Answers |
|---|---|---|
| `GET /health` | the OCR service | contract version 1 and all 26 categories, so the app's capability checks pass |
| `POST /process-batch` | the OCR service | a **canned leaderboard** for the requested category, naming the demo alliance's members — whatever image was uploaded. Its diagnostics say so (`engine: demo-canned`), and that reaches the activity log. A mail carries "now" as its timestamp, so the participation import finds a recent occurrence |
| `/browser/dist/cool.html` | Collabora | a **picture** of the document the app asked to open (by the file id in `WOPISrc`), in the requested theme, with a "Demo" band; a file that is not a seeded sample gets a generic "no preview" picture. It never calls the app back |

It needs no database: the boards and the file list come from `demo.Manifest()`, a pure function of
the seed. It is configured by flags, not environment variables. `-frame-ancestors` is the app's
origin, the only page allowed to embed the document picture.

Point a seeded app at it with the OCR backend in local mode — the demo never auto-detects a
screen, so a category is always sent — and Collabora at its host:

```bash
go run ./cmd/demo-seed --db /tmp/demo.db --uploads /tmp/demo-uploads --worker-url http://localhost:9090
OCR_BACKEND_MODE=local COLLABORA_DOMAIN=localhost:9090 \
  DATABASE_PATH=/tmp/demo.db STORAGE_PATH=/tmp/demo-uploads go run ./cmd/server
```

`deploy/demo/Dockerfile.fixtures` builds it (`docker build -f deploy/demo/Dockerfile.fixtures .`
from the repository root). The document pictures in `internal/fixtures/img/` were captured from a
real Collabora opening the seeded `.docx` and `.xlsx`, light and dark; retake them if the sample
files change.

## Demo mode

`DEMO_MODE=true` turns the ordinary image into the public demo. Nothing else about the image
changes; there is no demo build.

| Variable | Default | Meaning |
|---|---|---|
| `DEMO_MODE` | *(unset)* | `true` for the public demo. |
| `DEMO_FIXTURES_URL` | *(unset)* | The fixtures service's URL, stored as the OCR worker URL when the database is seeded. |
| `DEMO_FIXTURES_ORIGIN` | *(unset)* | The fixtures service's origin, for the demo's `frame-src` / `connect-src`. |
| `DEMO_RESET_HOURS` | `6` | Hours before the instance restarts itself for a fresh database. `0` disables. |
| `DEMO_PASSWORD` | random | The six accounts' password. A deployment secret: no page and no document shows it. |

None of these is in `.env.example`, which is for operator installs.

What demo mode does:

- **Seeds an empty database at boot**, before first-run setup is decided, anchored on today. A
  seed that fails stops the boot (a failed revision, rather than a setup page nobody can claim).
- **Signs visitors in with "Try as …" buttons** on the login page, one per seeded account
  (`POST /api/demo/login`, demo mode only, its own rate limit). The password form is not shown.
- **Refuses what a visitor must not reach**, with a 403 whose body is "This action is disabled
  in the demo." — and the page shows that sentence. Writes only; every page still renders:
  account management and password changes, invite and reset links, security settings and
  credentials, the whole mobile API, file upload / create / delete and Collabora's write-back,
  member deletion, and every route that reaches LastRank (the scheduler is off as well). The
  permissions matrix and ordinary settings stay editable — they are what the demo is for — and
  the reset bounds what a visitor can change.
- **Records nothing about visitors**: no login history (address, browser, location).
- **Keeps the sign-in page the app's own**: no admin-set login banner is shown or stored.
- **Sets the security headers itself** — the ones `deploy/Caddyfile` sets on an ordinary install
  (a test keeps the two identical), with the fixtures origin allowed to be framed.
- **Resets**: after `DEMO_RESET_HOURS` the process restarts; the next request starts a fresh
  instance with an empty disk, which seeds itself. An idle instance scaled to zero resets sooner,
  so the banner says "at least every N hours and on every cold start".

Every visitor is an administrator by design. That is the demo's feature, not a vulnerability to
report; `SECURITY.md` says what is in scope, and it applies to the demo as to any install.

### Running it locally

```bash
go run ./cmd/demo-fixtures -port 9090 -frame-ancestors http://localhost:8080 &
DEMO_MODE=true DEMO_FIXTURES_URL=http://localhost:9090 DEMO_FIXTURES_ORIGIN=http://localhost:9090 \
  OCR_BACKEND_MODE=local COLLABORA_DOMAIN=localhost:9090 \
  DATABASE_PATH=/tmp/demo-live.db STORAGE_PATH=/tmp/demo-live go run ./cmd/server
```

Delete `/tmp/demo-live.db` to start over.

## Deploying the public demo

Two Cloud Run services, specs in `deploy/demo/`. **Redeploy the demo with every app release**, so
it never runs an image older than what an operator would install.

1. **Copy the released app image into Artifact Registry** (Cloud Run cannot pull from GHCR):
   `docker pull ghcr.io/shodiwarmic/lastwar-alliance-manager:vX.Y.Z`, tag it
   `<REGION>-docker.pkg.dev/<PROJECT>/lastwar-demo/alliance-manager:vX.Y.Z`, push.
2. **Build and push the fixtures image** from the same tag:
   `docker build -f deploy/demo/Dockerfile.fixtures --build-arg APP_VERSION=vX.Y.Z -t <...>/demo-fixtures:vX.Y.Z .`
3. **Secrets** in Secret Manager: `lastwar-demo-session-key` (`openssl rand -hex 32`) and
   `lastwar-demo-password`. **A runtime service account of the demo's own** (`<RUNTIME-SA>` in
   both specs) holding only `roles/secretmanager.secretAccessor` on those two secrets — never the
   default compute account, which is Editor on the project.
4. **Deploy the fixtures service first**, then the app, with the hosts filled into both specs:
   `gcloud run services replace deploy/demo/cloudrun-fixtures.yaml`, then `cloudrun-app.yaml`.
   Both are public (`allUsers` invoker).
5. **Measure, on the first deploy:** `GET /api/demo/whoami` from two different networks — set
   `TRUSTED_PROXY_COUNT` so `client_ip` is the caller's real address, then redeploy; two
   sign-ins from one client must not share a rate-limit bucket with another client. Record one
   visit's egress and CPU against the table below, and how often an idle instance actually
   resets.
6. **A budget alert** on the billing account, filtered on the `app: lastwar-demo` label both
   specs carry. `max-instances 1` caps compute; nothing caps egress except the alert.
7. **The launch gate**: the security review of open issues is recorded in the project's private
   tracker before the demo is listed anywhere.

**Measured on the first deploy (v2.2.0, 2026-10-07):** `TRUSTED_PROXY_COUNT=1` — Google's front
end appends one `X-Forwarded-For` entry, and a forged leading entry does not move `client_ip`.
One visit (sign-in and seven pages, cold caches) is about 164 requests and **470 KiB** — an upper
bound, since it counts the CDN-hosted libraries the demo does not serve.

**Cost** (free tier per billing account per month, as of 2026-09-30): 180,000 vCPU-s, 360,000
GiB-s, 2M requests and **1 GB egress from North America** are free; at under 0.5 MB a visit,
egress is the first limit, around 2,000 visits a month. A scaled-to-zero service costs nothing
while idle. Artifact Registry's free 0.5 GB holds both images; the demo's repository keeps the
three most recent of each.
