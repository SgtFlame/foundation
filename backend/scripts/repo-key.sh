#!/usr/bin/env bash
# Makes the first-party repository's signing key (forge 20f-1):
#
#   scripts/repo-key.sh <keys-dir>
#
# writes its public half to <keys-dir>/repo-signing.asc, which serve
# --control-plane puts in every instance's desired state as the
# repository's keyring, and its secret half to <keys-dir>/repo-signing.key
# (0600). Move the secret into Vault and delete the file:
#
#   scripts/secrets/set.sh server/repo-signing-key < <keys-dir>/repo-signing.key
#   shred -u <keys-dir>/repo-signing.key
#
# then sign with scripts/repo.sh --key-file - fed from Vault.
set -euo pipefail
die() { echo "repo-key.sh: $*" >&2; exit 1; }
keys=${1:?usage: repo-key.sh <keys-dir>}
[[ -d "$keys" ]] || die "$keys is not a directory"
[[ ! -e "$keys/repo-signing.asc" ]] || die "$keys/repo-signing.asc exists: move it away to make a new key"
command -v gpg >/dev/null || die "gpg is needed"
home=$(mktemp -d)
trap 'rm -rf "$home"' EXIT
chmod 700 "$home"
gpg --homedir "$home" --batch --quiet --passphrase '' --quick-gen-key "Axiom repository <packages@axiom.invalid>" ed25519 sign never
gpg --homedir "$home" --batch --armor --export > "$keys/repo-signing.asc"
(umask 077; gpg --homedir "$home" --batch --armor --export-secret-keys > "$keys/repo-signing.key")
echo "wrote $keys/repo-signing.asc and $keys/repo-signing.key: move the .key into Vault"
