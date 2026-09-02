#!/usr/bin/env bash
# Cuts a release of one module for the module registry (see registry/ and
# backend/internal/registryclient): packages modules/<id> into a tarball,
# uploads it as a GitHub Release asset, and updates index.json with the
# new version/severity/changelog/checksum entry registry-server serves to
# every client deployment.
#
# Requires: gh (authenticated against this repo), git, sha256sum, tar.
# Run from the repo root.
#
# Usage:
#   scripts/publish-module.sh <module-id> <severity> "<changelog>"
#
#   severity is one of: security | recommended | optional
#
# Example:
#   scripts/publish-module.sh compute-mesh security "Fixes a device-group bootstrap race on first install."
set -euo pipefail

if [ $# -ne 3 ]; then
  echo "usage: $0 <module-id> <security|recommended|optional> <changelog>" >&2
  exit 1
fi

MODULE_ID="$1"
SEVERITY="$2"
CHANGELOG="$3"

case "$SEVERITY" in
  security|recommended|optional) ;;
  *) echo "severity must be one of: security, recommended, optional" >&2; exit 1 ;;
esac

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULE_DIR="$REPO_ROOT/modules/$MODULE_ID"
INDEX_FILE="$REPO_ROOT/index.json"

if [ ! -f "$MODULE_DIR/manifest.yaml" ]; then
  echo "no such module: $MODULE_DIR/manifest.yaml not found" >&2
  exit 1
fi
if ! command -v gh >/dev/null; then
  echo "gh (GitHub CLI) is required — https://cli.github.com" >&2
  exit 1
fi
if ! command -v jq >/dev/null; then
  echo "jq is required" >&2
  exit 1
fi

VERSION=$(grep -E '^version:' "$MODULE_DIR/manifest.yaml" | head -1 | sed -E 's/version:\s*"?([^"]*)"?/\1/')
if [ -z "$VERSION" ]; then
  echo "could not read version from $MODULE_DIR/manifest.yaml" >&2
  exit 1
fi

MODULE_NAME=$(grep -E '^name:' "$MODULE_DIR/manifest.yaml" | head -1 | sed -E 's/name:\s*"?([^"]*)"?/\1/')
MODULE_CATEGORY=$(grep -E '^category:' "$MODULE_DIR/manifest.yaml" | head -1 | sed -E 's/category:\s*"?([^"]*)"?/\1/')

TAG="${MODULE_ID}-v${VERSION}"
ASSET_NAME="${MODULE_ID}-${VERSION}.tar.gz"
WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT
ASSET_PATH="$WORKDIR/$ASSET_NAME"

# Package everything in the module's own directory — manifest.yaml,
# docker-compose.yaml, and anything else it references (nginx templates,
# etc.) — the same tree extractBundle unpacks back into modules/<id> on
# the client side.
tar -czf "$ASSET_PATH" -C "$MODULE_DIR" .
SHA256=$(sha256sum "$ASSET_PATH" | cut -d' ' -f1)

echo "publishing $MODULE_ID v$VERSION ($SEVERITY) as $TAG"

if gh release view "$TAG" >/dev/null 2>&1; then
  echo "release $TAG already exists — delete it first if you need to republish this exact version" >&2
  exit 1
fi

gh release create "$TAG" "$ASSET_PATH" \
  --title "$MODULE_ID v$VERSION" \
  --notes "$CHANGELOG"

# Update index.json: replace this module's entry (or add it, for a
# brand-new module) and leave every other module's entry untouched.
TMP_INDEX="$WORKDIR/index.json"
jq --arg id "$MODULE_ID" \
   --arg name "$MODULE_NAME" \
   --arg category "$MODULE_CATEGORY" \
   --arg version "$VERSION" \
   --arg severity "$SEVERITY" \
   --arg changelog "$CHANGELOG" \
   --arg sha256 "$SHA256" \
   '.modules = ([.modules[] | select(.id != $id)] + [{
     id: $id, name: $name, category: $category,
     latest_version: $version, severity: $severity,
     changelog: $changelog, sha256: $sha256
   }])' "$INDEX_FILE" > "$TMP_INDEX"
mv "$TMP_INDEX" "$INDEX_FILE"

cd "$REPO_ROOT"
git add index.json
git commit -m "Publish $MODULE_ID v$VERSION ($SEVERITY)"
git push

echo "done — $MODULE_ID v$VERSION is now live in the module registry"
