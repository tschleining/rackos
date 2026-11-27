
#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

BINARY="$ROOT_DIR/bin/rackosd-web"

if [ "$(uname -s)" != "Linux" ]; then
    echo "Isolated HTTPS browser tests currently require Linux." >&2
    exit 1
fi

if ! command -v certutil >/dev/null 2>&1; then
    echo "NSS certutil is required." >&2
    echo "On Ubuntu, install libnss3-tools." >&2
    exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
    echo "curl is required." >&2
    exit 1
fi

if [ ! -x "$BINARY" ]; then
    echo "Integrated appliance executable not found." >&2
    echo "Run ./scripts/build-appliance.sh first." >&2
    exit 1
fi

if [ "${NODE_TLS_REJECT_UNAUTHORIZED:-}" = "0" ]; then
    echo "Node TLS certificate verification must be enabled." >&2
    exit 1
fi

# Preserve the installed Playwright browser location
# before replacing HOME and XDG directories.
BROWSER_PATH="${PLAYWRIGHT_BROWSERS_PATH:-${XDG_CACHE_HOME:-$HOME/.cache}/ms-playwright}"

TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/rackos-console-https.XXXXXX")"

APPLIANCE_DIR="$TEST_ROOT/appliance"
BROWSER_HOME="$TEST_ROOT/browser-home"

CA_PATH="$APPLIANCE_DIR/management-pki/ca.crt"

BASE_URL="https://127.0.0.1:18780"

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

mkdir -p "$BROWSER_HOME"

echo "Starting isolated RackOS HTTPS browser-test appliance"

RACKOS_DATA_DIR="$APPLIANCE_DIR" \
RACKOS_HTTP_ADDR=127.0.0.1:18780 \
RACKOS_GRPC_ADDR=127.0.0.1:18751 \
RACKOS_MANAGEMENT_TLS=true \
RACKOS_PERFORMANCE_RETENTION=0s \
RACKOS_LOG_LEVEL=info \
"$BINARY" &

PID=$!

echo "Waiting for verified HTTPS readiness"

READY=false
ATTEMPT=0

while [ "$ATTEMPT" -lt 60 ]; do
    if ! kill -0 "$PID" 2>/dev/null; then
        echo "HTTPS appliance exited unexpectedly." >&2
        exit 1
    fi

    if [ -s "$CA_PATH" ] && \
        curl \
            --noproxy '*' \
            --silent \
            --fail \
            --max-time 2 \
            --cacert "$CA_PATH" \
            "$BASE_URL/readyz" >/dev/null 2>&1
    then
        READY=true
        break
    fi

    ATTEMPT=$((ATTEMPT + 1))
    sleep 1
done

if [ "$READY" != "true" ]; then
    echo "Verified HTTPS readiness check failed." >&2
    exit 1
fi

echo "Initializing disposable browser certificate trust"

export HOME="$BROWSER_HOME"

export XDG_CONFIG_HOME="$BROWSER_HOME/.config"
export XDG_CACHE_HOME="$BROWSER_HOME/.cache"
export XDG_DATA_HOME="$BROWSER_HOME/.local/share"

NSS_DB="$XDG_DATA_HOME/pki/nssdb"

mkdir -p "$NSS_DB"
mkdir -p "$HOME/.pki"

# Accommodate Chromium versions using the
# legacy NSS shared database location.
ln -s "$NSS_DB" "$HOME/.pki/nssdb"

certutil \
    -N \
    -d "sql:$NSS_DB" \
    --empty-password

certutil \
    -A \
    -d "sql:$NSS_DB" \
    -n "RackOS HTTPS Test CA" \
    -t "C,," \
    -i "$CA_PATH"

export PLAYWRIGHT_BROWSERS_PATH="$BROWSER_PATH"

# Node must load this certificate at startup.
export NODE_EXTRA_CA_CERTS="$CA_PATH"

export RACKOS_E2E_HTTPS=true

export NO_PROXY="127.0.0.1,localhost,::1"
export no_proxy="$NO_PROXY"

echo "Running browser tests with certificate verification enabled"

cd "$ROOT_DIR"

npm --prefix web run test:e2e
