#!/usr/bin/env bash
# The seed (forge 22o): the one step run by hand. On the machine it is to
# use (--adopt: on premise, or a machine the operator has) or in a new LXD
# VM on this machine (--provider lxd), it installs and initializes LXD,
# launches the first NATS instance and the server instance, installs the
# first-party packages from the apt repository on a channel, starts the
# first serve with the control plane, makes its first administrator (the
# password from stdin) and commits a first configuration describing what it
# made, marked applied. Run again, it changes nothing. Every later change
# is a version applied by blueprints.
#
#   seed.sh --adopt --site lab --repo https://repo.example/axiom --repo-key key.asc --admin tony < password
#   seed.sh --provider lxd --name axiom-seed --site lab ... < password
#
# Options: --site ID (lower case, digits, dashes), --site-name NAME,
# --repo URL, --repo-key FILE (the repository's ASCII-armoured key),
# --channel NAME (the suite, default stable), --package NAME (the server
# package, default axiom-server; forge-server for Axiom), --server-bin NAME
# (its binary, default foundation), --import-cmd "CMD" (default
# "<server-bin> infra import"; "forge infra-import" with Forge's), --admin
# NAME, --image IMAGE (default ubuntu:24.04). azure and digitalocean
# providers come with their parts (22l, 22m).
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
source "$here/lib.sh"

mode="" provider="" vm_name=axiom-seed site="" site_name="" repo="" repo_key="" channel=stable
package=axiom-server server_bin=foundation import_cmd="" admin="" image=ubuntu:24.04
args=("$@")
while [[ $# -gt 0 ]]; do
  case "$1" in
    --adopt) mode=adopt; shift ;;
    --provider) mode=provider; provider=$2; shift 2 ;;
    --name) vm_name=$2; shift 2 ;;
    --site) site=$2; shift 2 ;;
    --site-name) site_name=$2; shift 2 ;;
    --repo) repo=$2; shift 2 ;;
    --repo-key) repo_key=$2; shift 2 ;;
    --channel) channel=$2; shift 2 ;;
    --package) package=$2; shift 2 ;;
    --server-bin) server_bin=$2; shift 2 ;;
    --import-cmd) import_cmd=$2; shift 2 ;;
    --admin) admin=$2; shift 2 ;;
    --image) image=$2; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done
[[ -n "$mode" ]] || die "choose --adopt or --provider lxd"
[[ "$site" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$ ]] || die "--site is lower case letters, digits and dashes"
[[ -n "$repo" && -r "$repo_key" ]] || die "--repo URL and a readable --repo-key FILE are needed"
[[ "$admin" =~ ^[A-Za-z0-9._-]+$ ]] || die "--admin names the first administrator"
[[ "$package" =~ ^[a-z0-9][a-z0-9.+-]+$ && "$channel" =~ ^[a-z0-9][a-z0-9.-]*$ ]] || die "--package and --channel are Debian names"
command -v jq >/dev/null || die "jq is needed"
site_name=${site_name:-$site}
import_cmd=${import_cmd:-"$server_bin infra import"}

# The administrator's password, from stdin (typed without echo on a terminal).
if [[ -t 0 ]]; then
  IFS= read -rsp "Password for $admin: " password
  echo >&2
else
  IFS= read -r password || true
fi
[[ ${#password} -ge 8 ]] || die "the password needs at least 8 characters"

if [[ "$mode" == provider ]]; then
  case "$provider" in
    lxd)
      # The first host is a VM on this machine's LXD; the seed then runs
      # in it as --adopt.
      ensure_snap lxd
      ensure_lxd_init "$(printf 'storage_pools:\n- name: default\n  driver: dir\nnetworks:\n- name: lxdbr0\n  type: bridge\n  config: {ipv4.address: auto, ipv6.address: none}\nprofiles:\n- name: default\n  devices:\n    root: {path: /, pool: default, type: disk}\n    eth0: {name: eth0, network: lxdbr0, type: nic}')"
      ensure_instance "$vm_name" "$image" --vm -c security.nesting=true
      for f in seed.sh lib.sh; do
        sync_file "$vm_name" "/root/seed/$f" 0755 <"$here/$f" || true
      done
      sync_file "$vm_name" /root/seed/repo.asc 0644 <"$repo_key" || true
      inner=()
      for (( i = 0; i < ${#args[@]}; i++ )); do
        case "${args[i]}" in
          --provider|--name|--repo-key) i=$((i + 1)) ;;
          *) inner+=("${args[i]}") ;;
        esac
      done
      printf '%s\n' "$password" | on "$vm_name" /root/seed/seed.sh --adopt --repo-key /root/seed/repo.asc "${inner[@]}"
      exit 0
      ;;
    azure|digitalocean|fly) die "the $provider provider comes with its part (22l to 22n); make the host there and run --adopt on it" ;;
    *) die "unknown provider $provider" ;;
  esac
fi

[[ $(id -u) == 0 || -n "${SEED_NOT_ROOT:-}" ]] || die "run the seed as root"
nats="nats-$site-1" server="server-$site-1" url=http://localhost:8080

# LXD, with a directory pool (ZFS later, with the cluster's storage, 22h).
ensure_snap lxd
ensure_lxd_init "$(printf 'storage_pools:\n- name: default\n  driver: dir\nnetworks:\n- name: lxdbr0\n  type: bridge\n  config: {ipv4.address: auto, ipv6.address: none}\nprofiles:\n- name: default\n  devices:\n    root: {path: /, pool: default, type: disk}\n    eth0: {name: eth0, network: lxdbr0, type: nic}')"

# The first NATS instance and the server instance, each with the
# first-party repository on the channel.
key=$(cat "$repo_key")
for name in "$nats" "$server"; do
  ensure_instance "$name" "$image"
  changed=0
  sync_file "$name" /etc/apt/keyrings/axiom.asc 0644 <<<"$key" && changed=1
  sync_file "$name" /etc/apt/sources.list.d/axiom.sources 0644 <<SRC && changed=1
Types: deb
URIs: $repo
Suites: $channel
Components: main
Signed-By: /etc/apt/keyrings/axiom.asc
SRC
  if (( changed )); then
    on "$name" apt-get update >/dev/null
  fi
done
ensure_packages "$nats" nats-server axiomd
ensure_unit "$nats" nats-server
ensure_packages "$server" "$package" axiomd

# The first serve, with the control plane (its own JetStream until 22i's
# bootstrap moves it onto the NATS instance).
if sync_file "$server" /etc/systemd/system/axiom-seed-serve.service 0644 <<UNIT; then
[Unit]
Description=Axiom serve, made by the seed
After=network-online.target

[Service]
ExecStart=/usr/bin/$server_bin serve --control-plane --addr :8080 --data-dir /var/lib/axiom --kb-root /var/lib/axiom/kb
Restart=on-failure
StateDirectory=axiom

[Install]
WantedBy=multi-user.target
UNIT
  on "$server" systemctl daemon-reload
  on "$server" systemctl restart axiom-seed-serve
fi
ensure_unit "$server" axiom-seed-serve
wait_ready "$server" "$url"

# The first administrator: user 1, made Owner of the root tenant when serve
# starts again with the control plane.
if [[ $(on "$server" curl -fsS "$url/api/v1/identity/status" | jq -r '.singleUser') == true ]]; then
  log "making the administrator $admin"
  jq -n --arg u "$admin" --arg p "$password" '{tenantName: "Default", username: $u, displayName: $u, password: $p}' |
    on "$server" curl -fsS -o /dev/null -X POST -H 'Content-Type: application/json' --data-binary @- "$url/api/v1/identity/setup"
  on "$server" systemctl restart axiom-seed-serve
  wait_ready "$server" "$url"
fi

# The first configuration: what the seed made, committed as version 1 and
# marked applied.
draft=/var/lib/axiom-seed/draft
sync_file "$server" "$draft/config.yaml" 0644 <<YAML || true
profiles:
  - id: server
    name: Server
    kind: container
    packages: [$package, axiomd]
    units:
      - name: serve
        kind: serve
        services: [all]
YAML
sync_file "$server" "$draft/sites/$site.yaml" 0644 <<YAML || true
site:
  id: $site
  name: "$site_name"
  onPremise: true
cluster:
  id: $site
  site: $site
  storage: dir
  natsReplicas: 1
hostPools:
  - id: seed
    site: $site
    size: seed
    count: 1
YAML
sync_file "$server" "$draft/placements.yaml" 0644 <<YAML || true
placements:
  - id: server
    profile: server
    sites:
      - site: $site
        count: 1
YAML
read -r -a import <<<"$import_cmd"
printf '%s\n' "$password" | on "$server" "${import[@]}" --server "$url" --username "$admin" --message "The seed" --applied "$draft" >&2

address=$(lxc list "$server" -c 4 --format csv | cut -d' ' -f1)
log "done: sign in as $admin at http://${address:-<the server instance>}:8080"
