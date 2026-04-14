#!/bin/bash
set -euo pipefail

# Vendors the tanzubench benchmark suite into src/tanzubench/ for BOSH packaging.
# This is run at development time, not at tile build time.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
BENCH_REPO="${1:?Usage: vendor-tanzubench.sh <path-to-tanzubench-repo>}"

echo "Vendoring tanzubench from $BENCH_REPO..."

TARGET="$REPO_DIR/src/tanzubench"
rm -rf "$TARGET"
mkdir -p "$TARGET"

# Copy the benchmark suite (tools, tests, schema)
cp -r "$BENCH_REPO/tools" "$TARGET/"
cp -r "$BENCH_REPO/tests" "$TARGET/"
cp -r "$BENCH_REPO/schema" "$TARGET/"

# Pre-build the web leaderboard
echo "Building web leaderboard..."
(cd "$BENCH_REPO/web" && npm ci && npm run build)
mkdir -p "$TARGET/web"
cp -r "$BENCH_REPO/web/out" "$TARGET/web/out"

# Vendor Python deps (jsonschema, pyyaml) for air-gap
# Strip opencode from agentic frameworks (no Linux CLI binary for air-gap)
find "$TARGET/tests/agentic" -name "*.yaml" -exec sed -i.bak "s/frameworks: \[opencode, aider, custom\]/frameworks: [aider, custom]/" {} \;
find "$TARGET/tests/agentic" -name "*.bak" -delete
echo "Stripped opencode from agentic frameworks"

# Replace pip install pytest with PYTHONPATH setup (pytest is vendored)
find "$TARGET/tests/agentic" -name "*.yaml" -exec sed -i.bak   "s|python3 -m pip install -q pytest|export PYTHONPATH=/var/vcap/packages/tanzubench/python-lib:\$PYTHONPATH 2>/dev/null || true|" {} \;
find "$TARGET/tests/agentic" -name "*.bak" -delete
echo "Replaced pip install with PYTHONPATH setup in agentic tests"

echo "Vendoring Python dependencies..."
pip download jsonschema pyyaml -d "$TARGET/python-deps/" --no-deps 2>/dev/null || true

echo "Vendored to $TARGET ($(du -sh "$TARGET" | cut -f1))"
