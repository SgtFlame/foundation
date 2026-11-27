#!/usr/bin/env bash
# Builds the first-party Debian packages (forge 20f-1), from binaries built
# with scripts/build.sh:
#
#   scripts/build.sh -o out/axiomd ./cmd/axiomd && scripts/build.sh -o out/cli
#   scripts/package.sh 1.4.0
#
# makes out/packages/axiomd_<v>_<arch>.deb (the daemon, with its agent and
# host units, not enabled: cloud-init or the operator enables one) and
# axiom-server_<v>_<arch>.deb (the one serve binary as /usr/bin/foundation,
# which the agent's axiom-serve@ units run). Each postinst only reloads
# systemd; nothing prompts. Forge builds its own server package the same
# way, from its serve:
#
#   scripts/package.sh 1.4.0 --server ../../forge/backend/out/cli --server-name forge-server
#
# Options: --out DIR, --axiomd BIN, --server BIN, --server-name NAME,
# --arch ARCH (dpkg's own by default), --only axiomd|server.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

die() { echo "package.sh: $*" >&2; exit 1; }
version=${1:-}
[[ "$version" =~ ^[0-9][A-Za-z0-9.+~-]*$ ]] || die "usage: package.sh <version> [options] (a Debian version starting with a digit)"
shift
out=out/packages axiomd=out/axiomd server=out/cli server_name=axiom-server only=
arch=$(dpkg --print-architecture 2>/dev/null || echo amd64)
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) out=$2; shift 2 ;;
    --axiomd) axiomd=$2; shift 2 ;;
    --server) server=$2; shift 2 ;;
    --server-name) server_name=$2; shift 2 ;;
    --arch) arch=$2; shift 2 ;;
    --only) only=$2; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done
[[ "$server_name" =~ ^[a-z0-9][a-z0-9.+-]+$ ]] || die "$server_name is not a package name"
command -v dpkg-deb >/dev/null || die "dpkg-deb is needed"
mkdir -p "$out"

# build <name> <description> <binary> <installed as> [extra control lines] -- [unit files]
build() {
  local name=$1 desc=$2 bin=$3 dest=$4 extra=$5; shift 5
  [[ -x "$bin" ]] || die "$bin is not built (scripts/build.sh)"
  local root
  root=$(mktemp -d)
  trap 'rm -rf "$root"' RETURN
  install -D -m 0755 "$bin" "$root$dest"
  for unit in "$@"; do
    install -D -m 0644 "$unit" "$root/lib/systemd/system/$(basename "$unit")"
  done
  mkdir -p "$root/DEBIAN"
  local size
  size=$(du -sk --exclude=DEBIAN "$root" | cut -f1)
  {
    echo "Package: $name"
    echo "Version: $version"
    echo "Architecture: $arch"
    echo "Maintainer: Axiom <packages@axiom.invalid>"
    echo "Installed-Size: $size"
    echo "Section: admin"
    echo "Priority: optional"
    [[ -n "$extra" ]] && printf '%s\n' "$extra"
    echo "Description: $desc"
  } > "$root/DEBIAN/control"
  cat > "$root/DEBIAN/postinst" <<'POST'
#!/bin/sh
set -e
if [ -d /run/systemd/system ]; then systemctl daemon-reload || true; fi
POST
  chmod 0755 "$root/DEBIAN/postinst"
  dpkg-deb --root-owner-group -Zxz --build "$root" "$out/${name}_${version}_${arch}.deb" >/dev/null
  echo "$out/${name}_${version}_${arch}.deb"
}

if [[ -z "$only" || "$only" == axiomd ]]; then
  build axiomd "Axiom control plane daemon (axiomd host, axiomd agent)" "$axiomd" /usr/bin/axiomd "" \
    packaging/axiomd-agent.service packaging/axiomd-host.service
fi
if [[ -z "$only" || "$only" == server ]]; then
  extra=""
  if [[ "$server_name" != axiom-server ]]; then
    extra=$'Provides: axiom-server\nConflicts: axiom-server\nReplaces: axiom-server'
  fi
  build "$server_name" "Axiom server (serve)" "$server" /usr/bin/foundation "$extra"
fi
