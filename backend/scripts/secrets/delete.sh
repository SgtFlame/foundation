#!/usr/bin/env bash
# Deletes a secret's current version, after asking. Vault keeps its
# earlier versions, so `vault kv undelete` can bring it back.
#
#   delete.sh server/old-api-key
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_vault

name=${1:?usage: delete.sh <name>}
if [[ "$name" == server/db-key ]]; then
  log "the server cannot start without server/db-key"
  exit 1
fi
if ! vault_api GET "$mount/metadata/$name" >/dev/null; then
  log "there is no secret $name"
  exit 1
fi
read -rp "Delete $name? [y/N] " answer
[[ "$answer" == [yY]* ]] || { log "kept $name"; exit 0; }
vault_api DELETE "$mount/data/$name" >/dev/null
log "deleted $name (its versions are kept)"
