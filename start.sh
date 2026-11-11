#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
atlas_python=""
for candidate in python3.12 python3.13 python3; do
  if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 12) and sys.version_info[:3] != (3, 14, 1) else 1)' >/dev/null 2>&1; then
    atlas_python="$candidate"
    break
  fi
done
if [ -z "$atlas_python" ]; then
  echo "Установите Python 3.12 или новее. Рекомендуется Python 3.12; версия 3.14.1 не поддерживается NetworkX." >&2
  exit 1
fi
if [ ! -d .venv ]; then "$atlas_python" -m venv .venv; fi
if [ ! -f .venv/atlas-requirements ] || ! cmp -s backend/requirements.txt .venv/atlas-requirements; then
  .venv/bin/python -m pip install -r backend/requirements.txt
  cp backend/requirements.txt .venv/atlas-requirements
fi
if [ ! -f frontend/dist/index.html ]; then
  if ! command -v npm >/dev/null 2>&1; then
    echo "Для сборки интерфейса установите Node.js 22 LTS с npm." >&2
    exit 1
  fi
  (cd frontend && npm ci && npm run build)
fi
echo "Семантический атлас: http://127.0.0.1:8765"
echo "Остановка: Ctrl+C. Документация API: http://127.0.0.1:8765/docs"
cd backend
exec ../.venv/bin/python -m uvicorn app.main:app --host 127.0.0.1 --port 8765

