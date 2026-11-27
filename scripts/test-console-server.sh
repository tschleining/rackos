
#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

BINARY="$ROOT_DIR/bin/rackosd-web"

if [ ! -x "$BINARY" ]; then
    echo "Integrated appliance executable not found." >&2
    echo "Run ./scripts/build-appliance.sh first." >&2
    exit 1
fi

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/rackos-console.XXXXXX")"

PID=""

cleanup() {
    if [ -n "$PID" ]; then
        kill -TERM "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null || true
    fi

    rm -rf "$TEST_ROOT"
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "Starting isolated RackOS browser-test appliance"

RACKOS_DATA_DIR="$TEST_ROOT" \
RACKOS_HTTP_ADDR=127.0.0.1:18780 \
RACKOS_GRPC_ADDR=127.0.0.1:18751 \
RACKOS_PERFORMANCE_RETENTION=0s \
RACKOS_LOG_LEVEL=info \
"$BINARY" &

PID=$!

wait "$PID"
