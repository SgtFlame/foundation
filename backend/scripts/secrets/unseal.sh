#!/usr/bin/env bash
# Unseals a server started with --vault-addr: makes a single-use SecretID
# for its AppRole, wrapped so it is only readable once and only for two
# minutes, posts the wrapping token (and the role ID) to the server, and
# waits until it is ready.
#
#   unseal.sh [server-url]   (default http://localhost:8080)
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_vault

server=${1:-http://localhost:8080}
state=$(curl -sS "$server/api/v1/bootstrap/status" | jq -r '.state') || { log "no server at $server"; exit 1; }
if [[ "$state" != sealed && "$state" != degraded ]]; then
  log "the server is $state, not sealed"
  exit 1
fi

role_id=$(vault_api GET "auth/approle/role/$role/role-id" | jq -r '.data.role_id')
wrapped=$(VAULT_WRAP_TTL=120s vault_api POST "auth/approle/role/$role/secret-id" '{}' | jq -r '.wrap_info.token')
answer=$(WRAPPED="$wrapped" ROLE_ID="$role_id" jq -n '{token: env.WRAPPED, roleId: env.ROLE_ID}' |
  curl -sS -X POST -H 'Content-Type: application/json' --data-binary @- -w '\n%{http_code}' "$server/api/v1/bootstrap/unseal")
code=${answer##*$'\n'}
if [[ "$code" != 200 ]]; then
  log "the server refused: $(jq -r '.message // .' <<<"${answer%$'\n'*}")"
  exit 1
fi

for _ in $(seq 120); do
  state=$(curl -sS "$server/api/v1/bootstrap/status" | jq -r '.state' 2>/dev/null || echo unreachable)
  if [[ "$state" == ready ]]; then
    log "the server is ready"
    exit 0
  fi
  sleep 1
done
log "the server is $state after two minutes; see its log"
exit 1
