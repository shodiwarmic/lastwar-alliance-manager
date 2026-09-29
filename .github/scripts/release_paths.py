"""What each path in the repository means for an OPERATOR receiving a release.

One definition, imported by two checks that must agree:

  * the release-level check (docker-publish.yml), which fails a patch or minor release whose
    diff reaches outside the image, and
  * the host-files manifest check (build-check.yml), which holds .github/host-files.manifest
    -- the list of files the host-files release asset carries -- to exactly the host set.

The lists were built from a full `git ls-files` census of the repository root, so the
unclassified branch fires only for a genuinely new path, never for a known file somebody
forgot to list. Adding a top-level path means deciding which list it joins.
"""

# Delivered inside the image. deploy/ is listed file by file on purpose: entrypoint.sh ships
# INSIDE the image, its siblings are host configuration.
IMAGE_DIRS = ('cmd/', 'internal/', 'templates/', 'static/', 'migrations/')
IMAGE_FILES = ('go.mod', 'go.sum', 'Dockerfile', 'deploy/entrypoint.sh')

# Delivered to the host. docker-compose.yml is host-affecting because an operator edits and
# keeps it. If the third-party image pins ever move out of it into .env, that classification
# has to be re-decided rather than assumed.
HOST_DIRS = ('scripts/',)
HOST_FILES = ('docker-compose.yml', 'docker-compose.local-ocr.yml',
              'deploy/Caddyfile', 'deploy/docker-compose.override.yml.example',
              '.env.example')

# Delivered to nobody. .github/ is neutral deliberately: a workflow change asks nothing of any
# host -- it runs only in CI, and the operator pulls the same image either way. Provenance of
# the artifact is guarded by PR review, not by this classifier, and calling the pipeline
# host-affecting would force a major release for every CI tweak, which is how a guardrail
# erodes by annoyance. tests/ holds the host scripts' test runner: CI-only.
NEUTRAL_DIRS = ('docs/', '.github/', 'tests/')
NEUTRAL_FILES = ('README.md', 'CLAUDE.md', 'SECURITY.md', 'LICENSE',
                 'CHANGELOG.md', '.gitignore', '.dockerignore')

# Host-classified, but deliberately NOT in the host-files asset -- each with its reason. The
# manifest check fails on an entry here that is no longer tracked, so this list cannot rot.
NOT_SHIPPED = {
    'scripts/refresh-dev-db.sh': 'dev tooling: copies the production database to a dev box',
    'scripts/refresh-dev-db.conf.example': 'dev tooling: the config for the line above',
}


def classify(path):
    """'image', 'host', 'neutral', or None for a path none of the lists covers."""
    if path.startswith(IMAGE_DIRS) or path in IMAGE_FILES:
        return 'image'
    if path.startswith(NEUTRAL_DIRS) or path in NEUTRAL_FILES:
        return 'neutral'
    if path.startswith(HOST_DIRS) or path in HOST_FILES:
        return 'host'
    return None


def classify_diff(paths):
    """(host, unclassified) paths among a diff's, each sorted -- what the release-level check
    fails a patch or minor on."""
    host, unclassified = [], []
    for path in sorted(set(paths)):
        kind = classify(path)
        if kind == 'host':
            host.append(path)
        elif kind is None:
            unclassified.append(path)
    return host, unclassified


def read_manifest(text):
    """The paths in .github/host-files.manifest: one per line, # comments, blanks ignored."""
    paths = []
    for line in text.splitlines():
        line = line.split('#', 1)[0].strip()
        if line:
            paths.append(line)
    return paths
