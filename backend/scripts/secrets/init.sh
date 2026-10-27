#!/usr/bin/env bash
# Prepares Vault for the server, and is safe to run again:
#   - the KV v2 mount (FOUNDATION_VAULT_MOUNT, default foundation);
#   - the foundation-server policy: read the bootstrap bundle (server/*),
#     and read and write secret settings (config/*) and the other server
#     secrets, which the Secrets editor maintains;
#   - the AppRole (FOUNDATION_VAULT_ROLE, default foundation): single-use
#     SecretIDs that live 10 minutes, and a periodic token renewed hourly;
#   - server/db-key, a new random database key, unless there is one.
# It prints the role ID, for serve --vault-role-id.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_vault

if ! vault_api GET sys/mounts | jq -e --arg m "$mount/" '.data[$m]' >/dev/null; then
  vault_api POST "sys/mounts/$mount" '{"type": "kv", "options": {"version": "2"}}' >/dev/null
  log "enabled the KV v2 mount $mount"
fi
if ! vault_api GET sys/auth | jq -e '.data["approle/"]' >/dev/null; then
  vault_api POST sys/auth/approle '{"type": "approle"}' >/dev/null
  log "enabled AppRole"
fi

policy=$(cat <<HCL
path "$mount/data/server/*"     { capabilities = ["create", "read", "update", "delete"] }
path "$mount/metadata/server"   { capabilities = ["list"] }
path "$mount/metadata/server/*" { capabilities = ["read", "list", "update"] }
path "$mount/data/config/*"     { capabilities = ["create", "read", "update", "delete"] }
path "$mount/metadata/config/*" { capabilities = ["read", "list", "update"] }
HCL
)
vault_api PUT sys/policies/acl/foundation-server "$(jq -n --arg p "$policy" '{policy: $p}')" >/dev/null
log "wrote the foundation-server policy"

vault_api POST "auth/approle/role/$role" '{
  "token_policies": ["foundation-server"],
  "secret_id_ttl": "10m",
  "secret_id_num_uses": 1,
  "token_period": "1h",
  "bind_secret_id": true
}' >/dev/null
log "wrote the $role AppRole"

if vault_api GET "$mount/data/server/db-key" >/dev/null; then
  log "server/db-key exists; rotate it with rotate-db-key.sh"
else
  head -c 32 /dev/urandom | base64 | kv_put server/db-key >/dev/null
  log "made server/db-key"
fi

role_id=$(vault_api GET "auth/approle/role/$role/role-id" | jq -r '.data.role_id')
echo "role ID: $role_id"
