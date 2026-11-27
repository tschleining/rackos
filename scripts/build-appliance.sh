#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

cd "$ROOT_DIR"

FRONTEND_DIST="$ROOT_DIR/web/apps/console/dist"
EMBED_DIST="$ROOT_DIR/internal/appliance/webconsole/dist"

echo "Installing locked frontend dependencies"
npm --prefix web ci

echo "Validating frontend types"
npm --prefix web run typecheck

echo "Building production web console"
npm --prefix web run build

if [ ! -s "$FRONTEND_DIST/index.html" ]; then
    echo "Frontend build did not produce index.html" >&2
    exit 1
fi

if [ ! -d "$FRONTEND_DIST/assets" ]; then
    echo "Frontend build did not produce an assets directory" >&2
    exit 1
fi

echo "Preparing embedded frontend assets"

rm -rf "$EMBED_DIST"
mkdir -p "$EMBED_DIST"

cp -R "$FRONTEND_DIST/." "$EMBED_DIST/"

echo "Building integrated RackOS appliance"

mkdir -p "$ROOT_DIR/bin"

go build \
    -tags webconsole \
    -trimpath \
    -o "$ROOT_DIR/bin/rackosd-web" \
    ./cmd/rackosd

echo ""
echo "Integrated appliance build complete:"
echo "$ROOT_DIR/bin/rackosd-web"