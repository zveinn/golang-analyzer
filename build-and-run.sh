#!/usr/bin/env bash
# Builds the React UI (embedded into the binary), the backend and the TCP
# client, then starts the server (UI on :1111, TCP intake on :1112).
set -euo pipefail
cd "$(dirname "$0")"

echo "==> building UI"
(
  cd ui
  if [ ! -d node_modules ]; then
    npm install
  fi
  npm run build
)

echo "==> building backend"
go build -o code-analyzer .
go build -o client/client ./client

# Stop a previous instance if one is holding the UI port — but only if it
# really is a code-analyzer process.
pid=$(ss -tlnp 2>/dev/null | grep ':1111 ' | grep -oP 'pid=\K[0-9]+' | head -1 || true)
if [ -n "${pid:-}" ] && [ "$(cat "/proc/$pid/comm" 2>/dev/null)" = "code-analyzer" ]; then
  echo "==> stopping previous instance (pid $pid)"
  kill "$pid"
  sleep 0.5
fi

echo "==> starting code-analyzer  (UI: http://localhost:1111, TCP intake: :1112)"
exec ./code-analyzer
