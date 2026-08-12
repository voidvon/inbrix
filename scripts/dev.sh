#!/usr/bin/env bash
# Start the Go API server and the Vite development server together.
#
# Vite serves the browser UI on :3000. Go serves API/auth routes on
# :3001. The port override is deliberate: config.toml remains usable for the
# normal single-binary mode, where Go listens on its configured port.

set -euo pipefail

repo_dir="$(cd "$(dirname "$0")/.." && pwd)"
cd "${repo_dir}"

backend_port="${LILMAIL_BACKEND_PORT:-3001}"
backend_url="${VITE_BACKEND_URL:-http://127.0.0.1:${backend_port}}"
backend_pid=""
frontend_pid=""

cleanup() {
	local status=$?
	trap - INT TERM EXIT
	if [[ -n "${backend_pid}" ]]; then
		kill "${backend_pid}" 2>/dev/null || true
	fi
	if [[ -n "${frontend_pid}" ]]; then
		kill "${frontend_pid}" 2>/dev/null || true
	fi
	wait "${backend_pid}" 2>/dev/null || true
	wait "${frontend_pid}" 2>/dev/null || true
	exit "${status}"
}

trap cleanup INT TERM EXIT

echo "Go backend: http://127.0.0.1:${backend_port}"
echo "Vite frontend: http://localhost:3000"

go run main.go -port "${backend_port}" &
backend_pid=$!

VITE_BACKEND_URL="${backend_url}" npm run dev &
frontend_pid=$!

# Bash on macOS does not provide `wait -n`, so poll both children and stop the
# other process as soon as either development server exits.
while true; do
	if ! kill -0 "${backend_pid}" 2>/dev/null; then
		if wait "${backend_pid}"; then status=0; else status=$?; fi
		exit "${status}"
	fi
	if ! kill -0 "${frontend_pid}" 2>/dev/null; then
		if wait "${frontend_pid}"; then status=0; else status=$?; fi
		exit "${status}"
	fi
	sleep 1
done
