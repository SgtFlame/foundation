# Sourced by the secrets scripts (documentation/Secrets.md). They talk
# to Vault's HTTP API with curl and jq, as the operator: VAULT_ADDR and the
# operator's own VAULT_TOKEN. Secret values and tokens never go on a command
# line, so they stay out of shell history and ps: values are read from
# stdin, and the token is passed to curl through a file descriptor.

shopt -s inherit_errexit # a failure inside $(…) stops the script too

mount="${FOUNDATION_VAULT_MOUNT:-foundation}"
role="${FOUNDATION_VAULT_ROLE:-foundation}"

log() { echo "$(basename "$0"): $*" >&2; }

need_vault() {
  if [[ -z "${VAULT_ADDR:-}" || -z "${VAULT_TOKEN:-}" ]]; then
    log "set VAULT_ADDR and VAULT_TOKEN (your own token, not the server's)"
    exit 1
  fi
  command -v jq >/dev/null || { log "jq is needed"; exit 1; }
}

# vault_api METHOD PATH [JSON]: prints the response body. Returns 4 for 404
# and fails with Vault's errors on any other error status. Set
# VAULT_WRAP_TTL to have the response wrapped.
vault_api() {
  local method=$1 path=$2 data=${3:-}
  local headers="X-Vault-Token: $VAULT_TOKEN"
  [[ -n "${VAULT_WRAP_TTL:-}" ]] && headers+=$'\n'"X-Vault-Wrap-TTL: $VAULT_WRAP_TTL"
  local args=(-sS -X "$method" -w '\n%{http_code}')
  [[ -n "$data" ]] && args+=(-H 'Content-Type: application/json' --data-binary @-)
  local out
  out=$(printf '%s' "$data" | curl "${args[@]}" -H @<(printf '%s\n' "$headers") "$VAULT_ADDR/v1/$path") || return 1
  local code=${out##*$'\n'} body=${out%$'\n'*}
  if [[ "$code" == 404 ]]; then
    return 4
  fi
  if (( code >= 400 )); then
    log "Vault answered $code for $method $path: $(jq -r '.errors | join("; ")' <<<"$body" 2>/dev/null || echo "$body")"
    return 1
  fi
  printf '%s' "$body"
}

# kv_put NAME: writes stdin as the secret NAME's value, recording the
# operator as its writer, and prints the new version.
kv_put() {
  local name=$1 json version
  json=$(jq -n --rawfile v /dev/stdin '{data: {value: ($v | rtrimstr("\n"))}}')
  if [[ $(jq -r '.data.value' <<<"$json") == "" ]]; then
    log "the value is empty"
    return 1
  fi
  version=$(vault_api POST "$mount/data/$name" "$json" | jq -r '.data.version') || return 1
  vault_api POST "$mount/metadata/$name" "$(jq -n --arg by "operator:${USER:-unknown}" '{custom_metadata: {updated_by: $by}}')" >/dev/null
  echo "$version"
}

# kv_walk PREFIX: prints every secret name under PREFIX.
kv_walk() {
  local prefix=${1%/} keys key
  keys=$(vault_api LIST "$mount/metadata/$prefix" | jq -r '.data.keys[]') || return 0
  for key in $keys; do
    if [[ "$key" == */ ]]; then
      kv_walk "$prefix/${key%/}"
    else
      echo "$prefix/$key"
    fi
  done
}
