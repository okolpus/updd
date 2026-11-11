#!/usr/bin/env bash
# Builds (when needed) and starts Semantic Atlas on http://127.0.0.1:8765.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  for candidate in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go "$HOME/go/bin/go"; do
    if [ -x "$candidate" ]; then PATH="$(dirname "$candidate"):$PATH"; break; fi
  done
fi
if ! command -v go >/dev/null 2>&1; then
  echo "Нужен Go 1.22 или новее. Установите его: brew install go  (или https://go.dev/dl/), затем снова запустите start.sh." >&2
  exit 1
fi

if [ ! -f frontend/dist/index.html ]; then
  if ! command -v npm >/dev/null 2>&1; then
    echo "Для сборки интерфейса установите Node.js 22 LTS с npm." >&2
    exit 1
  fi
  (cd frontend && npm ci && npm run build)
fi

mkdir -p bin
# Go downloads the toolchain version from go.mod automatically if the installed one is older.
newest_source=$(find go.mod go.sum backend/cmd backend/internal frontend/dist frontend/embed.go -type f -newer bin/atlas 2>/dev/null | head -n 1 || true)
if [ ! -x bin/atlas ] || [ -n "$newest_source" ]; then
  echo "Сборка сервера…"
  GOTOOLCHAIN="${GOTOOLCHAIN:-auto}" go build -o bin/atlas ./backend/cmd/atlas
fi

echo "Семантический атлас: http://${ATLAS_ADDR:-127.0.0.1:8765}"
echo "Остановка: Ctrl+C."
exec ./bin/atlas
