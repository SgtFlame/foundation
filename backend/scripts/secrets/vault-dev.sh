#!/usr/bin/env bash
# Starts a development Vault (in memory, unsealed, with a known root token)
# in a Docker container on :8200, and prints what to export. For
# development only: its data is lost when the container stops.
#
#   vault-dev.sh        start it (or say it is running)
#   vault-dev.sh stop   remove it
set -euo pipefail

image=hashicorp/vault:1.17.6
name=foundation-vault
token=dev-root-token

log() { echo "vault-dev.sh: $*" >&2; }

if [[ "${1:-}" == stop ]]; then
  docker rm -f "$name" >/dev/null && log "removed $name"
  exit 0
fi

if [[ -z "$(docker ps -q -f name="^$name\$")" ]]; then
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run -d --name "$name" --cap-add IPC_LOCK -p 8200:8200 \
    -e VAULT_DEV_ROOT_TOKEN_ID="$token" -e VAULT_DEV_LISTEN_ADDRESS=0.0.0.0:8200 "$image" >/dev/null
  for _ in $(seq 30); do
    curl -fsS http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1 && break
    sleep 1
  done
  log "Vault ${image#*:} is running in $name"
else
  log "$name is already running"
fi
cat <<OUT
export VAULT_ADDR=http://127.0.0.1:8200
export VAULT_TOKEN=$token
OUT
