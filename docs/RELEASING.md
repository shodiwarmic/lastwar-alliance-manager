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
| **Major** `v1.2.3 → v2.0.0` | Anything reaching outside the image — compose files, scripts, `.env.example`, the Caddyfile | Run `scripts/update.sh` from a terminal: files on the host have to change. |

**The load-bearing distinction is image-only versus not**, and only that one is machine-checked
(see below). Patch and minor are treated identically by every piece of tooling here; nothing
audits whether a feature "deserved" its minor. Choose between them by judgement and don't agonise
— getting it wrong costs nothing, whereas calling a host-affecting change a patch costs an
operator a broken install.

### Published image tags

| Tag | Points at | Moves? |
|---|---|---|
| `vX.Y.Z` | That exact release | Never |
| `vX.Y` | The newest patch of that minor | Moves with each patch |
| `latest` | The newest release | Moves with each release |
| `edge` | The newest commit on `main` | Moves with every merge |

`latest` means **latest release**, not latest commit — an operator pulling it expects a released
thing. Development builds are `edge`, which is what a push to `main` publishes. `vX.Y` is for an
install that wants patches automatically but will never take a minor unattended.

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

1. **Write the `CHANGELOG.md` entry** for the version, linking its PR.
2. **Commit and push it to `main`.** The release is cut from a commit that exists upstream; a
   local-only commit fails the ancestry assertion.
3. **Wait for that commit's checks to go green** — both `Build & Test` and `Docker Build`. The
   docker job alone takes about ten minutes. The release workflow treats a pending or absent
   check as a failure, so this wait is not optional.
4. **Tag that exact commit and push the tag, from a personal clone:**
   ```bash
   git tag v1.2.3 <sha>
   git push origin v1.2.3
   ```
   > **Never push a release tag from an action using the default `GITHUB_TOKEN`.** A tag pushed
   > that way triggers no workflow at all, so the release would build nothing, fail nothing, and
   > look exactly like success.
5. **Watch the publish run.** It asserts ancestry and checks, runs the release-level check, then
   builds and pushes `vX.Y.Z`, `vX.Y` and `latest` for amd64 and arm64.
6. **Create the GitHub Release**: `gh release create v1.2.3 --notes "..."`. Together with
   `CHANGELOG.md` this is the artifact a prospective operator actually reads.
7. **Verify the published tags** on the GHCR package page, or with
   `docker manifest inspect ghcr.io/shodiwarmic/lastwar-alliance-manager:v1.2.3` — which also
   confirms both architectures are present.

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
   genuinely new path.

Major releases skip step 5 entirely: promising nothing about paths is what that level is for.

Adding a new top-level path to the repository means adding it to one of the three lists in that
step, deciding what it asks of an operator who receives the release.

## Open question: fixes that never touch a board

The project workflow moves work through a GitHub Project, and a release is cut at the end of one.
A one-line fix made outside that flow has no board to end, and it is not yet decided whether such
a fix gets its own patch release immediately or waits for the next project's.

This is recorded as open rather than answered. Record the first real occurrence here; build
nothing until it has been felt twice.
