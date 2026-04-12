#!/bin/bash
set -euo pipefail

VERSION="${1:?Usage: build-tile.sh <version>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== Building TanzuBench tile v${VERSION} ==="

# Step 1: Build the BOSH release
echo "Building BOSH release..."
cd "$REPO_DIR"
bosh create-release --force --version="${VERSION}" --tarball="resources/tanzubench-release.tgz"

# Step 2: Download dependency releases (BPM + routing)
echo "Downloading BPM release..."
[ -f resources/bpm-release.tgz ] || \
  curl -sL "https://bosh.io/d/github.com/cloudfoundry/bpm-release?v=1.4.23" \
    -o resources/bpm-release.tgz

echo "Downloading routing release..."
[ -f resources/routing-release.tgz ] || \
  curl -sL "https://bosh.io/d/github.com/cloudfoundry/routing-release?v=0.354.0" \
    -o resources/routing-release.tgz

# Step 3: Patch tile.yml with version
sed -i.bak "s/^  version: .*/  version: '${VERSION}'/" tile.yml
rm -f tile.yml.bak

# Step 4: Build the tile
echo "Running tile-generator..."
tile build "${VERSION}"

# Step 5: Post-build fix for errand boolean bug
# (tile-generator writes 'false' as string instead of boolean)
PIVOTAL="product/tanzubench-${VERSION}.pivotal"
if [ -f "$PIVOTAL" ]; then
  echo "Patching errand boolean bug..."
  TMPDIR=$(mktemp -d)
  cd "$TMPDIR"
  unzip -q "$REPO_DIR/$PIVOTAL"
  find . -name "*.yml" -exec sed -i.bak \
    "s/run_post_deploy_errand_default: 'false'/run_post_deploy_errand_default: false/g" {} \;
  find . -name "*.bak" -delete
  zip -q -r "$REPO_DIR/$PIVOTAL" .
  cd "$REPO_DIR"
  rm -rf "$TMPDIR"
fi

echo "=== Tile built: $PIVOTAL ==="
ls -lh "$PIVOTAL"
