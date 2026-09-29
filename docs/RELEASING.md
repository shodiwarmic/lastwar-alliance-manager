# Releasing

How a version of the Alliance Manager gets cut, what each level promises the operator running
it, and what to do when a release fails. Releases are cut **by hand**, deliberately: this is a
self-hosted app with a small number of installs, and a human deciding "this is a release" is
worth more than automation that decides it for us.

Operators reading this for how to *take* a release want
[DEPLOYMENT.md → Update Procedure](DEPLOYMENT.md#6-update-procedure) instead.

## Versions and what they promise

Versions are SemVer with the `v` kept — `v1.2.3` — in every spelling: the git tag, the image
tag, the `APP_VERSION` pin in `.env`, the Admin page, and the GitHub Release. Nothing ever
translates between `v1.2.3` and `1.2.3`.

| Level | What changed | What the operator has to do |
|---|---|---|
| **Patch** `v1.2.3 → v1.2.4` | Image only — Go code, templates, static assets, migrations | Pull the new image. Nothing else. |
| **Minor** `v1.2.3 → v1.3.0` | Image only, with new functionality | Pull the new image. Nothing else. |
| **Major** `v1.2.3 → v2.0.0` | Anything reaching outside the image — compose files, scripts, `.env.example`, the Caddyfile | Run `./scripts/manage.sh update` from a terminal: files on the host have to change, and it fetches them with the image. |

**The load-bearing distinction is image-only versus not**, and only that one is machine-checked
(see below). Patch and minor are treated identically by every piece of tooling here; nothing
audits whether a feature "deserved" its minor. Choose between them by judgement and don't agonise
— getting it wrong costs nothing, whereas calling a host-affecting change a patch costs an
operator a broken install.

### The host-files asset

What an install needs besides the image — the files `.github/host-files.manifest` lists — ships
as a release asset, `host-files.tar.gz`, with its `host-files.tar.gz.sha256`. The tag's publish
run builds both with `.github/scripts/build-host-files.sh` and attaches them to a **draft**
GitHub Release. The build is deterministic, so running the script on the tag locally produces
the same bytes and the same checksum.

`./scripts/manage.sh update` runs in two stages, and the first is always the copy already on the
host — the previous release's. A release's changes to the rest of the updater take effect in the
run that delivers them. **A change to stage 1 itself** (the `apply` argument list, or
`resolve_latest_release` / `download_asset` / `unpack_asset` in `scripts/lib/common.sh`) **takes
effect one release late, by design**; say so in that release's notes if it matters. (Before
v2.0.0 the whole of `update.sh` behaved this way, and a release changing it had to be run twice.)

### Published image tags

| Tag | Points at | Moves? |
|---|---|---|
| `vX.Y.Z` | That exact release | Never |
| `vX.Y` | The newest patch of that minor | Moves with each patch |
| `latest` | The newest release | Moves when a release is **published** (not when it is tagged) |
| `edge` | The newest commit on `main` | Moves with every merge |

`latest` means **latest release**, not latest commit — an operator pulling it expects a released
thing. Development builds are `edge`, which is what a push to `main` publishes. `vX.Y` is for an
install that wants patches automatically but will never take a minor unattended.

`latest` moves in `.github/workflows/release-published.yml`, when the draft release is published
— a manifest re-tag of the already-built `vX.Y.Z`, not a rebuild. That keeps the `latest` image
and the `releases/latest` host-files download on the same release at every moment; were `latest`
moved at tag time, an install following it could pull the new image while its host files were
still the previous release's.

### The never-rebuild rule

**Once a version tag has published an image, that tag is never rebuilt, re-pushed or
overwritten.** The base images (`golang:1.27-bookworm`, `debian:bookworm-slim`) are mutable tags,
so rebuilding the same source a month later can produce a genuinely different image — and a
version that resolves to two different digests over time is exactly the ambiguity this whole
scheme exists to remove. If something is wrong with a published release, the fix is a new
version, not a repair of the old one.

The one exception is a tag that **never published** — a release that failed its pre-build checks.
Nothing can have pulled it, so it is free to delete and re-push. See the remedies below.

### A Dockerfile contract change is a major, by hand

`Dockerfile` is classified image-affecting, so a change to it passes the automated check at patch
level. That is right for almost every edit to it — but a change to the *contract* the container
presents, its `EXPOSE`d port or its `ENTRYPOINT`, is host-visible whatever the classifier says.
Those are rare and reviewed; treat one as a major yourself. The check is a guardrail against
accidents, not a proof system.

## Cutting a release

The ordering matters — the release workflow refuses to build a tag whose commit is not on `main`
or whose checks have not concluded successfully, and a tag placed too early fails on both counts.

The changelog entry travels with the change rather than being written at tag time. That keeps the
release path free of any direct push to `main`, which today works only because admin enforcement
on the branch protection happens to be switched off — turn that on and a tag-time changelog commit
becomes impossible, discovered mid-release.

1. **The `CHANGELOG.md` entry rides in the PR**, written by whoever raises it — they have the
   context to say what changed and why, which a close-out reconstructing it from a diff does not.
   Its heading names the version being cut.
2. **Merge the PR.** The **merge commit** is what gets tagged, not the PR's head commit.
3. **Wait for the merge commit's checks to go green** on `main` — both `Build & Test` and
   `Docker Build`. The docker job alone takes a couple of minutes. This is a *different run* from
   the one that passed on the PR: a squash merge creates a new commit, and the release workflow
   asserts on the checks of the commit it is building. It treats pending or absent as failure, so
   this wait is not optional.
4. **Tag that exact commit and push the tag, from a personal clone:**
   ```bash
   git tag v1.2.3 <sha>
   git push origin v1.2.3
   ```
   > **Never push a release tag from an action using the default `GITHUB_TOKEN`.** A tag pushed
   > that way triggers no workflow at all, so the release would build nothing, fail nothing, and
   > look exactly like success.
5. **Watch the publish run.** It asserts ancestry and checks, runs the release-level check, then
   builds and pushes `vX.Y.Z` and `vX.Y` for amd64 and arm64. Its second job, **Release
   assets**, builds `host-files.tar.gz` and creates a **draft** GitHub Release carrying it and its
   checksum.
6. **Publish the draft.** Open it, replace the placeholder with this version's section of
   `CHANGELOG.md`, and publish. Publishing is what moves `latest`
   (`release-published.yml` — watch it go green) and makes the asset the `releases/latest`
   download that `manage.sh update` and the install instructions fetch. Together with
   `CHANGELOG.md` the release is the artifact a prospective operator actually reads.
7. **Verify.** The tags on the GHCR package page, or
   `docker manifest inspect ghcr.io/shodiwarmic/lastwar-alliance-manager:v1.2.3` — which also
   confirms both architectures are present — and that `latest` now names the same digest. The
   asset: `.github/scripts/build-host-files.sh v1.2.3 /tmp/asset v1.2.3` from a clone must print
   the checksum the release shows.

## When a release fails

The remedy depends on *why*, and the difference is whether the version was burned.

**Failed the ancestry or checks assertion** — nothing was built and nothing was published, so the
version is untouched. Fix the cause (push the commit, or wait for its checks), then delete and
re-push the *same* tag:

```bash
git push --delete origin v1.2.3
git tag -d v1.2.3
# ...once the checks are green...
git tag v1.2.3 <sha> && git push origin v1.2.3
```

**Failed the release-level check** — the diff genuinely reaches outside the image. Delete the tag
as above, then either re-tag the same commit as the next **major**, or fix the change and tag
afresh. Do not "fix" it by widening the classifier's lists; the lists describe what an operator
must do, and editing them to make a release pass is editing the promise rather than keeping it.

**Failed after the image was published** — the version is burned. Cut the next one. See the
never-rebuild rule.

**Image published, but the Release assets job failed** — the version is fine; the release is just
not finished. Re-run the failed job ("Re-run failed jobs" on that run): it rebuilds only the
asset, never the image, and it completes an existing draft rather than creating a second one. If
that cannot work, build the asset on the tag yourself — the build is deterministic, so it is the
same file CI would have made — and attach it:

```bash
.github/scripts/build-host-files.sh v1.2.3 dist v1.2.3
gh release create v1.2.3 --draft --verify-tag --title v1.2.3 --notes "…" \
  dist/host-files.tar.gz dist/host-files.tar.gz.sha256      # or `gh release upload` onto the draft
```

Never publish a release without both assets: `manage.sh update` would resolve it as the latest and
fail to download it (safely — it changes nothing — but every install would stop updating).

**Published, but `latest` did not move** — re-run `release-published.yml` for that release. It only
re-tags an existing manifest.

## What the release-level check actually does

It lives in the tag-triggered half of `.github/workflows/docker-publish.yml`, runs before the
build, and fails the release rather than the PR — there is no tag at PR time, so there is nothing
to check then.

1. The tagged commit is an ancestor of `origin/main`.
2. Its `Build & Test` and `Docker Build` runs concluded `success`.
3. The previous `v*` tag resolves. If it does not, the release fails — **unless** the tag is
   `v1.0.0` and it is the repository's first, which is exempted explicitly and skips steps 4–5
   rather than comparing against nothing.
4. The level is derived from the SemVer delta, not from anything anyone wrote down.
5. For a patch or minor, every path in the diff is classified image-affecting, host-affecting or
   neutral. A host-affecting path fails the release, naming the paths. An **unclassified** path
   also fails, demanding a decision — the lists cover the whole tree, so that only fires for a
   genuinely new path. The lists live in `.github/scripts/release_paths.py`.

Major releases skip step 5 entirely: promising nothing about paths is what that level is for.

Adding a new top-level path to the repository means adding it to one of the three lists in
`.github/scripts/release_paths.py`, deciding what it asks of an operator who receives the release.
A new **host** file also needs a decision about the asset: list it in `.github/host-files.manifest`,
or in `NOT_SHIPPED` with its reason. Build & Test's **Host-files manifest check** fails until one
of the two says so.

## Open question: fixes that never touch a board

The project workflow moves work through a GitHub Project, and a release is cut at the end of one.
A one-line fix made outside that flow has no board to end, and it is not yet decided whether such
a fix gets its own patch release immediately or waits for the next project's.

This is recorded as open rather than answered. Record the first real occurrence here; build
nothing until it has been felt twice.

**First occurrence, 2026-09-20.** A two-line fix to `static/schedule.js`: every server-event save
failed as "Network error", because the response variable shadowed the request payload and put that
payload in the temporal dead zone for the block using it. The bug had been live since the previous
project merged, so it shipped inside v1.0.0. It was given its own patch release rather than held
for the next project's — the failure was officer-visible, the fix was image-only, and waiting
would have meant knowingly shipping a broken feature for however long the next board ran. One
occurrence, not a rule.
