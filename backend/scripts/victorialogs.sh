#!/usr/bin/env bash
# Runs VictoriaLogs for development on :9428, downloading the pinned release
# into third_party/victorialogs/ on first use. Its data lives in
# <data-dir>/victorialogs (--data-dir, default ~/.foundation, as for serve).
# Other arguments go to victoria-logs, e.g. -retentionPeriod=30d.
# serve.sh does not start it; turn it on in Settings › Logging, or pass
# serve --vlogs-url http://localhost:9428.
set -euo pipefail

version=v1.53.0

backend="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dir="$backend/third_party/victorialogs"
bin="$dir/victoria-logs-$version"

data_dir="$HOME/.foundation"
args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --data-dir) data_dir="$2"; shift 2 ;;
    --data-dir=*) data_dir="${1#*=}"; shift ;;
    *) args+=("$1"); shift ;;
  esac
done

log() { echo "victorialogs.sh: $*" >&2; }

if [[ ! -x "$bin" ]]; then
  case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) log "unsupported OS $(uname -s)"; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) log "unsupported architecture $(uname -m)"; exit 1 ;;
  esac
  name="victoria-logs-$os-$arch-$version"
  url="https://github.com/VictoriaMetrics/VictoriaLogs/releases/download/$version"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  log "downloading VictoriaLogs $version ($os-$arch)"
  curl -fsSL -o "$tmp/$name.tar.gz" "$url/$name.tar.gz"
  curl -fsSL -o "$tmp/checksums.txt" "$url/${name}_checksums.txt"
  (cd "$tmp" && grep " $name.tar.gz\$" checksums.txt | sha256sum -c --quiet -)
  tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
  mkdir -p "$dir"
  mv "$tmp/victoria-logs-prod" "$bin"
fi

mkdir -p "$data_dir/victorialogs"
log "VictoriaLogs $version on :9428, data in $data_dir/victorialogs"
exec "$bin" -httpListenAddr=:9428 -storageDataPath="$data_dir/victorialogs" "${args[@]}"
