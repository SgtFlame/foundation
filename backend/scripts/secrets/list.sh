#!/usr/bin/env bash
# Lists secrets under a prefix (default: all): names, versions, update
# times and who wrote them. Never values.
#
#   list.sh            every secret
#   list.sh server/    the bootstrap bundle
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
need_vault

prefix=${1:-}
names=()
if [[ -z "$prefix" ]]; then
  mapfile -t names < <(kv_walk server; kv_walk config)
else
  mapfile -t names < <(kv_walk "$prefix")
fi
printf '%-56s %7s  %-20s  %s\n' NAME VERSION UPDATED BY
for name in "${names[@]}"; do
  [[ -z "$name" ]] && continue
  vault_api GET "$mount/metadata/$name" | jq -r --arg n "$name" '.data as $d
    | ($d.versions[($d.current_version | tostring)]) as $v
    | [$n, ($d.current_version | tostring) + (if ($v.deletion_time // "") != "" then " (deleted)" else "" end),
       ($d.updated_time[:19] | sub("T"; " ")), ($d.custom_metadata.updated_by // "")]
    | @tsv' | while IFS=$'\t' read -r n v t by; do printf '%-56s %7s  %-20s  %s\n' "$n" "$v" "$t" "$by"; done
done
