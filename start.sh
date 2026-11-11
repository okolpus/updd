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

# --- Qwen через Ollama: каждый разбор текста идёт через неё -------------------
MODEL="${OLLAMA_MODEL:-qwen3:4b}"
OLLAMA_ADDR="${OLLAMA_URL:-http://127.0.0.1:11434}"
ollama_up() { curl -s -m 2 "$OLLAMA_ADDR/api/tags" >/dev/null 2>&1; }
if [ "${ATLAS_AI:-on}" != "off" ]; then
  if ! command -v ollama >/dev/null 2>&1; then
    for candidate in /opt/homebrew/bin/ollama /usr/local/bin/ollama /Applications/Ollama.app/Contents/Resources/ollama; do
      if [ -x "$candidate" ]; then PATH="$(dirname "$candidate"):$PATH"; break; fi
    done
  fi
  if ! command -v ollama >/dev/null 2>&1 && ! ollama_up; then
    if command -v brew >/dev/null 2>&1; then
      echo "Qwen: устанавливаю Ollama (один раз)…"
      brew install ollama || echo "Qwen: не удалось установить Ollama через brew."
    else
      echo "Qwen: установите Ollama с https://ollama.com/download и снова запустите start.sh." >&2
    fi
  fi
  if command -v ollama >/dev/null 2>&1; then
    if ! ollama_up && [ -z "${OLLAMA_URL:-}" ]; then
      echo "Qwen: запускаю Ollama…"
      nohup ollama serve >"${TMPDIR:-/tmp}/semantiq-ollama.log" 2>&1 &
      for _ in $(seq 1 20); do ollama_up && break; sleep 1; done
    fi
    if ollama_up; then
      if ! ollama list 2>/dev/null | awk 'NR>1 {print $1}' | grep -qx "$MODEL"; then
        echo "Qwen: скачиваю модель $MODEL (один раз, около 2,5 ГБ)…"
        ollama pull "$MODEL" || echo "Qwen: не удалось скачать $MODEL. Проверьте интернет и запустите: ollama pull $MODEL" >&2
      fi
      echo "Qwen: $MODEL подключена."
    else
      echo "Qwen: Ollama не отвечает. Журнал: ${TMPDIR:-/tmp}/semantiq-ollama.log" >&2
    fi
  fi
fi

# --- Порт: старый сервер (в том числе прежняя Python-версия) мешает запуску ---------
ADDR="${ATLAS_ADDR:-127.0.0.1:8765}"
PORT="${ADDR##*:}"
if command -v lsof >/dev/null 2>&1; then
  for pid in $(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN -t 2>/dev/null | sort -u); do
    cmd=$(ps -o command= -p "$pid" 2>/dev/null || true)
    case "$cmd" in
      *uvicorn*app.main:app*|*bin/atlas*)
        echo "Останавливаю прежний сервер атласа на порту $PORT (PID $pid)…"
        kill "$pid" 2>/dev/null || true
        for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
        kill -9 "$pid" 2>/dev/null || true
        ;;
      *)
        echo "Порт $PORT занят другой программой: $cmd" >&2
        echo "Закройте её или запустите атлас на другом порту: ATLAS_ADDR=127.0.0.1:8766 bash start.sh" >&2
        exit 1
        ;;
    esac
  done
fi

echo "Семантический атлас: http://${ATLAS_ADDR:-127.0.0.1:8765}"
echo "Остановка: Ctrl+C."
exec ./bin/atlas
