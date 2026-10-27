#!/usr/bin/env bash
# Builds and runs `out/cli serve`, rebuilding and restarting it whenever a Go
# file, go.mod or go.sum changes. A failed build leaves the running server
# alone. Arguments are passed to serve, e.g. scripts/serve.sh --addr :9090
set -uo pipefail

backend="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$backend"

# Local environment (GEMINI_API_KEY and so on); see setEnv.example.sh
if [[ -f scripts/setEnv.sh ]]; then
  echo "serve.sh: sourcing scripts/setEnv.sh" >&2
  source scripts/setEnv.sh
fi

cli=out/cli
next=out/cli.next
pid=""

log() { echo "serve.sh: $*" >&2; }

# build runs scripts/build.sh -o "$1", logging a heartbeat every 30s so a
# long build (minutes; far longer the first time, see build.sh) is not
# mistaken for a hang.
heartbeat=""
build() {
  local started now status
  started="$(date +%s)"
  log "building $1 (usually takes a few minutes)..."
  (
    while sleep 30 && kill -0 $$ 2>/dev/null; do
      now="$(date +%s)"
      log "still building $1 ($((now - started))s elapsed)..."
    done
  ) &
  heartbeat=$!
  scripts/build.sh -o "$1"
  status=$?
  kill "$heartbeat" 2>/dev/null
  wait "$heartbeat" 2>/dev/null
  heartbeat=""
  now="$(date +%s)"
  if [[ $status -eq 0 ]]; then
    log "built $1 in $((now - started))s"
  else
    log "build of $1 failed after $((now - started))s"
  fi
  return $status
}

start() {
  "$cli" serve "$@" &
  pid=$!
  log "started serve (pid $pid)"
}

stop() {
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null
    for _ in $(seq 50); do # up to 5s for a clean shutdown
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.1
    done
    kill -KILL "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
  fi
  pid=""
  if [[ -n "$heartbeat" ]]; then
    kill "$heartbeat" 2>/dev/null
    heartbeat=""
  fi
}

trap 'stop; exit 0' INT TERM
trap stop EXIT

# wait_for_change returns once a watched file changes, then lets a burst of
# saves settle.
if command -v inotifywait >/dev/null; then
  # inotifywait takes --exclude or --include, not both: exclude the build
  # output (which also keeps it from watching third_party's large tree) and
  # match Go files here.
  wait_for_change() {
    local file
    while file="$(inotifywait -q -r -e close_write,create,delete,move --format '%w%f' \
      --exclude '(^|/)(out|third_party|\.git)(/|$)' "$backend")"; do
      case "$file" in
        *.go | */go.mod | */go.sum) break ;;
      esac
    done
    sleep 0.3
  }
else
  log "inotifywait not found (install inotify-tools); polling every second"
  stamp="$(mktemp)"
  wait_for_change() {
    touch "$stamp"
    while [[ -z "$(find "$backend" \( -path "$backend/out" -o -path "$backend/third_party" -o -path "$backend/.git" \) -prune -o \
      \( -name '*.go' -o -name go.mod -o -name go.sum \) -newer "$stamp" -print -quit)" ]]; do
      sleep 1
    done
    sleep 0.3
  }
fi

until build "$cli"; do
  log "build failed; waiting for a change"
  wait_for_change
done
start "$@"

while true; do
  wait_for_change
  log "change detected, rebuilding"
  if build "$next"; then
    stop
    rm -f "$cli"
    mv "$next" "$cli"
    start "$@"
  else
    rm -f "$next"
    log "build failed; the running server is unchanged"
  fi
done
