#!/usr/bin/env bash
# scripts/seed-demo.sh
#
# Start inbrix in demo mode and (optionally) capture screenshots.
#
# Demo mode uses an in-memory mail client seeded with realistic messages — no
# IMAP server, no real credentials. The binary is configured via a temporary
# config.toml written to a temp directory so the real config is never touched.
#
# Usage:
#   scripts/seed-demo.sh              # start server in foreground (Ctrl-C to stop)
#   scripts/seed-demo.sh --screenshots # start, capture screenshots, then stop
#
# Requirements:
#   - Go toolchain and Node/npm (to build the binary if missing)
#   - Node 18+ and 'npm install' run once in scripts/ (for --screenshots)
#   - Playwright Chromium: cd scripts && npx playwright install chromium

set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="$REPO/inbrix"
TMP_DIR="$REPO/.demo-tmp"
TMP_CFG="$TMP_DIR/config.toml"
PORT="${INBRIX_PORT:-3099}"
BASE_URL="http://localhost:$PORT"

DEMO_EMAIL="${INBRIX_DEMO_EMAIL:-demo@inbrix.dev}"
DEMO_PASSWORD="${INBRIX_DEMO_PASSWORD:-demo}"

# Reproducible secrets (safe for local use; not for production).
JWT_SECRET="${INBRIX_JWT_SECRET:-inbrix-demo-jwt-secret-not-for-production}"
ENC_KEY="${INBRIX_ENC_KEY:-inbrix-demo-enc-key-32-bytes!!!!}"  # exactly 32 chars

DO_SCREENSHOTS=0
WITH_CALENDAR=0
for arg in "$@"; do
  case "$arg" in
    --screenshots)   DO_SCREENSHOTS=1 ;;
    --with-calendar) WITH_CALENDAR=1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Optional: a REAL CalDAV server, for the calendar screenshot.
#
# Demo mode fakes IMAP (handlers/api/democlient.go) but there is no fake
# CalDAV: handlers take a concrete *api.CalDAVClient that speaks to a real
# server, so the calendar route is registered only when [caldav] enabled is
# set, and the capture run has always skipped it with a 404.
#
# Hand-rolling a fake CalDAV endpoint was the obvious shortcut and is the
# wrong one — the screenshot would then be a picture of the fake, and this
# repo's claim is that its screens are real captures. So this runs Radicale,
# an actual CalDAV server, and inbrix talks to it over the wire exactly as
# it would to Fastmail or a self-hosted box.
#
# Opt-in, because it needs Python and Radicale which the rest of the pipeline
# does not: `--with-calendar`. Without it everything behaves as before.
CALDAV_PORT="${INBRIX_CALDAV_PORT:-5232}"
CALDAV_URL="http://127.0.0.1:${CALDAV_PORT}/demo/work"
CALDAV_PID=""

if [[ "$WITH_CALENDAR" == "1" ]]; then
  if ! python3 -c "import radicale" 2>/dev/null; then
    echo "[seed-demo] --with-calendar needs Radicale: pip install radicale" >&2
    exit 1
  fi
  echo "[seed-demo] Starting Radicale (real CalDAV) on port $CALDAV_PORT ..."
  rm -rf "$TMP_DIR/caldav"
  mkdir -p "$TMP_DIR/caldav/collections"
  cat > "$TMP_DIR/radicale.conf" <<RCONF
[server]
hosts = 127.0.0.1:${CALDAV_PORT}
[auth]
type = none
[storage]
filesystem_folder = $TMP_DIR/caldav/collections
RCONF
  python3 -m radicale --config "$TMP_DIR/radicale.conf" > "$TMP_DIR/radicale.log" 2>&1 &
  CALDAV_PID=$!
  for _ in $(seq 1 30); do
    if curl -s -o /dev/null -X PROPFIND "http://127.0.0.1:${CALDAV_PORT}/" -u demo:demo; then break; fi
    sleep 0.3
  done
  curl -s -o /dev/null -X MKCOL "${CALDAV_URL}/" -u demo:demo \
    -H "Content-Type: application/xml" --data '<?xml version="1.0" encoding="utf-8"?>
<mkcol xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav"><set><prop>
<resourcetype><collection/><C:calendar/></resourcetype><displayname>Work</displayname>
</prop></set></mkcol>'
  "$REPO/scripts/seed-calendar.sh" "$CALDAV_URL"
fi

# -----------------------------------------------------------------------
# Build binary if needed
# -----------------------------------------------------------------------
if [[ ! -f "$BINARY" ]]; then
  echo "[seed-demo] Building frontend and inbrix binary..."
  cd "$REPO" && npm run build && go build -o inbrix .
fi

# -----------------------------------------------------------------------
# Write temporary config
# -----------------------------------------------------------------------
mkdir -p "$TMP_DIR"
cat > "$TMP_CFG" <<TOML
[server]
port = $PORT
username_is_email = true

[imap]
server = "imap.example.com"
port   = 993

[smtp]
server = "smtp.example.com"
port   = 587
use_starttls = true

[cache]
folder = "$TMP_DIR/cache"

[jwt]
secret = "$JWT_SECRET"

[encryption]
key = "${ENC_KEY:0:32}"

[demo]
enabled  = true
email    = "$DEMO_EMAIL"
password = "$DEMO_PASSWORD"
TOML

if [[ "$WITH_CALENDAR" == "1" ]]; then
  cat >> "$TMP_CFG" <<TOML

[caldav]
enabled  = true
url      = "$CALDAV_URL"
auth     = "basic"
username = "demo"
password = "demo"
TOML
fi

echo "[seed-demo] Demo config written to $TMP_CFG"
echo "[seed-demo] Demo login URL (no credentials needed): $BASE_URL/demo-login"
echo "[seed-demo] Starting inbrix on $BASE_URL ..."

# -----------------------------------------------------------------------
# Start server
# -----------------------------------------------------------------------
SERVER_PID=""
cleanup() {
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
  fi
  # Radicale too, when --with-calendar started one — otherwise it outlives the
  # run and holds the port against the next capture.
  if [[ -n "${CALDAV_PID:-}" ]]; then
    kill "$CALDAV_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_DIR"
  echo "[seed-demo] Stopped."
}
trap cleanup EXIT INT TERM

# Run binary from TMP_DIR so it reads config.toml there
cd "$TMP_DIR"
"$BINARY" &
SERVER_PID=$!

# Wait for health check
DEADLINE=$((SECONDS + 15))
while [[ $SECONDS -lt $DEADLINE ]]; do
  if curl -sf "$BASE_URL/health" > /dev/null 2>&1; then
    break
  fi
  sleep 0.3
done

if ! curl -sf "$BASE_URL/health" > /dev/null 2>&1; then
  echo "[seed-demo] ERROR: server did not become ready within 15 s"
  exit 1
fi

echo "[seed-demo] Server ready."
echo "[seed-demo] Demo login URL: $BASE_URL/demo-login"

# -----------------------------------------------------------------------
# Screenshots (optional)
# -----------------------------------------------------------------------
if [[ $DO_SCREENSHOTS -eq 1 ]]; then
  echo "[seed-demo] Capturing screenshots..."
  cd "$REPO"

  if [[ ! -d "scripts/node_modules" ]]; then
    echo "[seed-demo] Installing Playwright..."
    cd scripts && npm install && cd ..
  fi

  INBRIX_EXTERNAL=1 \
  BASE_URL="$BASE_URL" \
  INBRIX_USERNAME="$DEMO_EMAIL" \
  INBRIX_PASSWORD="$DEMO_PASSWORD" \
  INBRIX_IMAP_SERVER="imap.example.com" \
  node scripts/demo-screenshots.mjs

  echo "[seed-demo] Screenshots written to docs/screenshots/"
else
  echo "[seed-demo] Server running. Press Ctrl-C to stop."
  wait "$SERVER_PID"
fi
