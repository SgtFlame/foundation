#!/usr/bin/env bash
# fly-lxd-proof.sh - forge 22n's proof: does LXD (or Incus) run inside a
# Fly Machine well enough to be a containers-only host?
#
# Starts one Ubuntu 24.04 Machine in an app of yours, installs Incus from
# Ubuntu's archive (a Machine has no systemd, so no snapd for the LXD
# snap), initializes it, launches one container, and reports what worked:
# cgroup v2, /dev/kvm, the storage drivers, networking and the container.
# The Machine is destroyed at the end, whatever happens.
#
# Usage: FLY_API_TOKEN=... fly-lxd-proof.sh --app <app> [--region ams] [--size performance-2x]
# Needs flyctl on the PATH. The token is read by flyctl from the
# environment and never printed.
set -euo pipefail

app="" region="ams" size="performance-2x"
while [[ $# -gt 0 ]]; do
	case "$1" in
	--app) app="$2"; shift 2 ;;
	--region) region="$2"; shift 2 ;;
	--size) size="$2"; shift 2 ;;
	-h | --help) sed -n '2,14p' "$0"; exit 0 ;;
	*) echo "unknown argument $1" >&2; exit 2 ;;
	esac
done
[[ -n "$app" ]] || { echo "--app is required" >&2; exit 2; }
command -v fly >/dev/null || { echo "flyctl (fly) is not on the PATH" >&2; exit 2; }
[[ -n "${FLY_API_TOKEN:-}" ]] || { echo "FLY_API_TOKEN is not set" >&2; exit 2; }

# The checks run inside the Machine; each prints "check <name> ok|fail <detail>".
read -r -d '' inner <<'INNER' || true
set -u
check() { printf 'check %s %s %s\n' "$1" "$2" "${3:-}"; }
fs=$(stat -fc %T /sys/fs/cgroup 2>/dev/null || echo none)
[[ "$fs" == cgroup2fs ]] && check cgroup2 ok "$fs" || check cgroup2 fail "$fs"
[[ -e /dev/kvm ]] && check kvm ok "/dev/kvm" || check kvm fail "no /dev/kvm"
check kernel ok "$(uname -r)"
export DEBIAN_FRONTEND=noninteractive
if apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq incus >/dev/null 2>&1; then
	check install ok "incus $(incus --version 2>/dev/null)"
else
	check install fail "apt-get install incus failed"; exit 0
fi
(incusd --group root >/var/log/incusd.log 2>&1 &)
for _ in $(seq 1 30); do incus info >/dev/null 2>&1 && break; sleep 1; done
incus info >/dev/null 2>&1 && check daemon ok "incusd answers" || { check daemon fail "$(tail -n 3 /var/log/incusd.log | tr '\n' ' ')"; exit 0; }
drivers=$(incus info | sed -n 's/^ *storage_supported_drivers://p' | head -c 300)
check storage ok "${drivers:-unknown}"
if incus admin init --auto >/dev/null 2>&1; then check init ok "--auto"; else check init fail "incus admin init --auto failed"; exit 0; fi
if incus launch images:ubuntu/24.04 proof >/dev/null 2>&1; then check launch ok "images:ubuntu/24.04"; else check launch fail "incus launch failed"; exit 0; fi
for _ in $(seq 1 30); do incus exec proof -- true >/dev/null 2>&1 && break; sleep 1; done
incus exec proof -- true >/dev/null 2>&1 && check exec ok "incus exec" || check exec fail "the container does not run commands"
addr=$(incus list proof -c 4 -f csv 2>/dev/null | head -n1)
[[ -n "$addr" ]] && check network ok "$addr" || check network fail "no IPv4 address"
incus exec proof -- getent hosts archive.ubuntu.com >/dev/null 2>&1 && check egress ok "DNS from the container" || check egress fail "no DNS from the container"
INNER

echo "starting a $size Machine in $app ($region)..."
id=$(fly machine run ubuntu:24.04 --app "$app" --region "$region" --vm-size "$size" --name lxd-proof --restart no --detach --json -- sleep infinity |
	sed -n 's/.*"id": *"\([0-9a-f]*\)".*/\1/p' | head -n1)
[[ -n "$id" ]] || { echo "the Machine did not start" >&2; exit 1; }
trap 'fly machine destroy "$id" --app "$app" --force >/dev/null 2>&1 || true; echo "destroyed Machine $id"' EXIT
fly machine wait "$id" --app "$app" --state started --wait-timeout 120 >/dev/null

payload=$(printf '%s' "$inner" | base64 -w0)
report=$(fly ssh console --app "$app" --machine "$id" --command "bash -c 'echo $payload | base64 -d | bash'" 2>/dev/null || true)
echo "$report" | grep '^check ' || { echo "no report came back" >&2; exit 1; }

if echo "$report" | grep -q '^check exec ok'; then
	echo "result: LXD (Incus) runs containers in a Fly Machine; record it in forge plan 022n and build infra.fly as containers-only"
else
	echo "result: LXD does not run well enough; decide between a Machine as one instance without LXD, or dropping Fly.io (forge plan 022n)"
fi
