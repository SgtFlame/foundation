#!/usr/bin/env bash
# Rotates the database key: a new server/db-key version, with the JWT
# signing key (and any secret setting still sealed) re-sealed under it, by
# `cli secrets rekey`. Stop serve first and start it again afterwards.
# Arguments go to the command, e.g. --data-dir.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_vault

backend="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cli="$backend/out/cli"
[[ -x "$cli" ]] || { log "build the CLI first: scripts/build.sh"; exit 1; }
FOUNDATION_VAULT_MOUNT="$mount" exec "$cli" secrets rekey --vault-mount "$mount" "$@"
