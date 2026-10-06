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
