#!/usr/bin/env bash
# Writes one secret, read from stdin (typed without echo on a terminal):
#
#   set.sh server/gemini-api-key
#   pass show gemini | set.sh server/gemini-api-key
#
# Names under server/ are the bootstrap bundle, read when the server
# unseals: server/gemini-api-key is the server's Gemini key, under any key
# set in Settings. A setting's secret is config/<scope>/<section>/<path>,
# but set those in Settings or the Secrets editor so the setting knows.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_vault

name=${1:?usage: set.sh <name>}
if [[ ! "$name" =~ ^(server|config)(/[A-Za-z0-9:._-]+)+$ ]]; then
  log "$name is not a secret name: use server/<name>"
  exit 1
fi
if [[ "$name" == server/db-key ]]; then
  log "rotate the database key with rotate-db-key.sh"
  exit 1
fi
if [[ -t 0 ]]; then
  IFS= read -rsp "Value for $name: " value
  echo >&2
  version=$(printf '%s' "$value" | kv_put "$name")
else
  version=$(kv_put "$name")
fi
log "wrote $name, version $version"
