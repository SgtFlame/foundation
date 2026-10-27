#!/usr/bin/env bash
# Builds the backend CLI with CGO, linking LanceDB's native library.
#
#   scripts/build.sh                 build out/cli
#   scripts/build.sh -o out/cli.next build to another path
#   scripts/build.sh --test [args]   run go test ./... (or args) with the same CGO env
#   scripts/build.sh --vet           run go vet ./... with the same CGO env
#
# The lancedb-go version pinned in go.mod is a commit after v0.1.2, the last
# release, because full-text (BM25) search is only on its main branch, and
# no prebuilt library exists for it. The first build therefore compiles
# liblancedb_go.a from the module's Rust sources (20-40 minutes, needing
# cargo and protoc; both are installed under ~/.cargo and third_party/ if
# missing) and caches it in third_party/lancedb/<version>/. Set
# CARGO_TARGET_DIR to put the multi-gigabyte Rust build somewhere roomier.
set -euo pipefail

PROTOC_VERSION=29.3
# The Rust toolchain for the native build. Newer compilers reject crates
# locked in lancedb-go's Cargo.lock (ethnum 1.5.2 fails on 1.99).
RUST_TOOLCHAIN=1.95.0

backend="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$backend"

out=out/cli
mode=build
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    --test) mode=test; shift; break ;;
    --vet) mode=vet; shift; break ;;
    *) echo "build.sh: unknown argument $1" >&2; exit 2 ;;
  esac
done

case "$(uname -s)" in
  Linux) os=linux; syslibs="-lm -ldl -lpthread" ;;
  Darwin) os=darwin; syslibs="-framework Security -framework CoreFoundation" ;;
  *) echo "build.sh: unsupported OS $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "build.sh: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

lancedb_dir="$(go list -m -f '{{.Dir}}' github.com/lancedb/lancedb-go)"
lancedb_version="$(go list -m -f '{{.Version}}' github.com/lancedb/lancedb-go)"
libdir="$backend/third_party/lancedb/$lancedb_version"
lib="$libdir/lib/${os}_${arch}/liblancedb_go.a"

# ensure_cargo puts cargo on PATH, installing a minimal rustup toolchain if needed.
ensure_cargo() {
  export PATH="$HOME/.cargo/bin:$PATH"
  if ! command -v cargo >/dev/null; then
    echo "build.sh: installing the Rust toolchain (rustup)..." >&2
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal --no-modify-path
  fi
  if command -v rustup >/dev/null; then
    rustup toolchain install --profile minimal "$RUST_TOOLCHAIN" >&2
    export RUSTUP_TOOLCHAIN="$RUST_TOOLCHAIN"
  fi
}

# ensure_protoc sets PROTOC, downloading protoc into third_party/ if needed;
# lance's build compiles protobuf definitions.
ensure_protoc() {
  if [[ -n "${PROTOC:-}" ]]; then return; fi
  if command -v protoc >/dev/null; then export PROTOC="$(command -v protoc)"; return; fi
  local dir="$backend/third_party/protoc/$PROTOC_VERSION"
  if [[ ! -x "$dir/bin/protoc" ]]; then
    local platform
    case "$os-$arch" in
      linux-amd64) platform=linux-x86_64 ;;
      linux-arm64) platform=linux-aarch_64 ;;
      darwin-amd64) platform=osx-x86_64 ;;
      darwin-arm64) platform=osx-aarch_64 ;;
    esac
    echo "build.sh: downloading protoc $PROTOC_VERSION..." >&2
    mkdir -p "$dir"
    curl -fsSL --retry 3 -o "$dir/protoc.zip" \
      "https://github.com/protocolbuffers/protobuf/releases/download/v$PROTOC_VERSION/protoc-$PROTOC_VERSION-$platform.zip"
    (cd "$dir" && unzip -q -o protoc.zip && rm protoc.zip)
  fi
  export PROTOC="$dir/bin/protoc"
}

if [[ ! -f "$lib" ]]; then
  echo "build.sh: building LanceDB's native library for lancedb-go $lancedb_version (once; this takes a while)..." >&2
  ensure_cargo
  ensure_protoc
  # The module cache is read-only, so build from a copy of the Rust sources
  src="$libdir/src"
  rm -rf "$src"
  mkdir -p "$src"
  cp -R "$lancedb_dir/rust/." "$src/"
  chmod -R u+w "$src"
  export CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-$backend/third_party/lancedb/target}"
  CARGO_PROFILE_RELEASE_DEBUG=0 cargo build --release --locked --manifest-path "$src/Cargo.toml"
  mkdir -p "$(dirname "$lib")"
  cp "$CARGO_TARGET_DIR/release/liblancedb_go.a" "$lib"
  rm -rf "$src"
fi

export CGO_ENABLED=1
export CGO_CFLAGS="-I$lancedb_dir/include"
export CGO_LDFLAGS="$lib $syslibs"

case "$mode" in
  build)
    mkdir -p "$(dirname "$out")"
    go build -o "$out" ./cli
    ;;
  test) go test "${@:-./...}" ;;
  vet) go vet "${@:-./...}" ;;
esac
