#!/usr/bin/env bash
# Builds and signs the first-party apt repository (forge 20f-1) for one
# channel, from packages scripts/package.sh built:
#
#   scripts/repo.sh <repo-dir> <stable|candidate> --key-file <armored secret key> <deb>...
#
# copies the packages into <repo-dir>/pool/main/, writes
# dists/<channel>/main/binary-<arch>/Packages(.gz) listing exactly the
# packages given, and a Release signed as InRelease and Release.gpg. Run it
# once per channel into one directory, then publish that directory with
# `cli controlplane repo publish <repo-dir>`.
#
# The signing key is the repository's (scripts/repo-key.sh makes it; keep
# its secret half in Vault as server/repo-signing-key): --key-file - reads
# it from stdin, and it is imported into a throwaway GnuPG home only.
# --unsigned skips signing, for tests that trust the repository anyway.
set -euo pipefail

die() { echo "repo.sh: $*" >&2; exit 1; }
[[ $# -ge 2 ]] || die "usage: repo.sh <repo-dir> <channel> (--key-file F | --unsigned) <deb>..."
repo=$1 channel=$2
shift 2
[[ "$channel" =~ ^(stable|candidate)$ ]] || die "the channel is stable or candidate"
key_file= unsigned= debs=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --key-file) key_file=$2; shift 2 ;;
    --unsigned) unsigned=1; shift ;;
    *.deb) debs+=("$1"); shift ;;
    *) die "unknown argument $1" ;;
  esac
done
[[ ${#debs[@]} -gt 0 ]] || die "no packages given"
[[ -n "$key_file" || -n "$unsigned" ]] || die "give --key-file (the repository key) or --unsigned"
for t in dpkg-deb sha256sum md5sum gzip; do command -v $t >/dev/null || die "$t is needed"; done

mkdir -p "$repo/pool/main"
dist="$repo/dists/$channel"
rm -rf "$dist"
declare -A arches=()
for deb in "${debs[@]}"; do
  [[ -f "$deb" ]] || die "$deb is not a file"
  name=$(dpkg-deb -f "$deb" Package) ver=$(dpkg-deb -f "$deb" Version) arch=$(dpkg-deb -f "$deb" Architecture)
  [[ "$arch" == all ]] || arches[$arch]=1
  cp -f "$deb" "$repo/pool/main/${name}_${ver}_${arch}.deb"
done
[[ ${#arches[@]} -gt 0 ]] || arches[amd64]=1

for arch in "${!arches[@]}"; do
  dir="$dist/main/binary-$arch"
  mkdir -p "$dir"
  : > "$dir/Packages"
  for deb in "${debs[@]}"; do
    name=$(dpkg-deb -f "$deb" Package) ver=$(dpkg-deb -f "$deb" Version) a=$(dpkg-deb -f "$deb" Architecture)
    [[ "$a" == "$arch" || "$a" == all ]] || continue
    file="pool/main/${name}_${ver}_${a}.deb"
    {
      dpkg-deb -f "$deb" | sed '/^$/d'
      echo "Filename: $file"
      echo "Size: $(stat -c %s "$repo/$file")"
      echo "MD5sum: $(md5sum < "$repo/$file" | cut -d' ' -f1)"
      echo "SHA256: $(sha256sum < "$repo/$file" | cut -d' ' -f1)"
      echo
    } >> "$dir/Packages"
  done
  gzip -9nk "$dir/Packages"
done

{
  echo "Origin: Axiom"
  echo "Label: Axiom"
  echo "Suite: $channel"
  echo "Codename: $channel"
  echo "Date: $(LC_ALL=C date -Ru)"
  echo "Architectures: $(echo "${!arches[@]}" | tr ' ' '\n' | sort | tr '\n' ' ' | sed 's/ $//')"
  echo "Components: main"
  echo "Acquire-By-Hash: no"
  for sum in MD5Sum SHA256; do
    echo "$sum:"
    tool=sha256sum; [[ $sum == MD5Sum ]] && tool=md5sum
    (cd "$dist" && find main -type f | sort | while read -r f; do
      printf ' %s %s %s\n' "$($tool < "$f" | cut -d' ' -f1)" "$(stat -c %s "$f")" "$f"
    done)
  done
} > "$dist/Release"

if [[ -z "$unsigned" ]]; then
  command -v gpg >/dev/null || die "gpg is needed"
  home=$(mktemp -d)
  trap 'rm -rf "$home"' EXIT
  chmod 700 "$home"
  if [[ "$key_file" == - ]]; then gpg --homedir "$home" --batch --quiet --import; else gpg --homedir "$home" --batch --quiet --import "$key_file"; fi
  gpg --homedir "$home" --batch --yes --quiet --clearsign -o "$dist/InRelease" "$dist/Release"
  gpg --homedir "$home" --batch --yes --quiet --armor --detach-sign -o "$dist/Release.gpg" "$dist/Release"
fi
echo "$dist"
