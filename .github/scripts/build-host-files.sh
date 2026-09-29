#!/bin/bash
# .github/scripts/build-host-files.sh VERSION OUTDIR [SOURCE_REF]
#
# Build the host-files release asset: OUTDIR/host-files.tar.gz and host-files.tar.gz.sha256.
#
# The tarball holds exactly the files .github/host-files.manifest lists, as they are at
# SOURCE_REF (default HEAD) -- not as they are in the working tree -- plus two generated ones:
#   HOST_FILES_VERSION  VERSION, which install.sh pins APP_VERSION to
#   .host-files.list    every path in the tarball, which the next update prunes against
#
# VERSION and SOURCE_REF are separate because a rehearsal build names a version
# (rehearsal-<sha>) that is not a git ref.
#
# Deterministic: sorted entries, fixed owner, the source commit's time as every mtime, and
# gzip without a name or timestamp -- so building the same tag anywhere gives the same bytes,
# and a local build is a way to check (or replace) the asset CI published.
set -euo pipefail

if [ $# -lt 2 ]; then
    echo "usage: $0 VERSION OUTDIR [SOURCE_REF]" >&2
    exit 2
fi
version=$1
out=$2
ref=${3:-HEAD}
if ! [[ $version =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    echo "not a usable version: $version" >&2
    exit 2
fi

# The repository this script lives in, wherever it is run from.
root=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mapfile -t files < <(git -C "$root" show "$ref:.github/host-files.manifest" \
    | sed 's/#.*//; s/[[:space:]]*$//' | grep -v '^$')
if [ ${#files[@]} -eq 0 ]; then
    echo "the manifest at $ref lists nothing" >&2
    exit 1
fi

mkdir -p "$work/tree"
for f in "${files[@]}"; do
    mkdir -p "$work/tree/$(dirname "$f")"
    git -C "$root" show "$ref:$f" > "$work/tree/$f"
    # The mode git records, not whatever the checkout happened to leave.
    if [ "$(git -C "$root" ls-tree "$ref" -- "$f" | cut -d' ' -f1)" = 100755 ]; then
        chmod 0755 "$work/tree/$f"
    else
        chmod 0644 "$work/tree/$f"
    fi
done
printf '%s\n' "$version" > "$work/tree/HOST_FILES_VERSION"
printf '%s\n' "${files[@]}" HOST_FILES_VERSION .host-files.list | LC_ALL=C sort > "$work/tree/.host-files.list"
chmod 0644 "$work/tree/HOST_FILES_VERSION" "$work/tree/.host-files.list"

mtime=$(git -C "$root" log -1 --format=%ct "$ref")
mkdir -p "$out"
(cd "$work/tree" && LC_ALL=C tar --format=gnu --owner=0 --group=0 --numeric-owner \
    --mtime="@$mtime" -cf - -T .host-files.list) | gzip -9 -n > "$out/host-files.tar.gz"
(cd "$out" && sha256sum host-files.tar.gz > host-files.tar.gz.sha256)
echo "Built $out/host-files.tar.gz ($version, from $(git -C "$root" rev-parse --short "$ref")):"
cat "$out/host-files.tar.gz.sha256"
