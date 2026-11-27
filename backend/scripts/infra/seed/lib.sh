# shellcheck shell=bash
# Sourced by seed.sh (forge 22o, documentation/Infrastructure.md). Every
# step checks before it acts, so a second run changes nothing. Secrets (the
# administrator's password) are read from stdin and passed on stdin, never
# on a command line or to the terminal.

shopt -s inherit_errexit # a failure inside $(…) stops the script too

log() { echo "seed: $*" >&2; }
die() { log "$*"; exit 1; }

# on NAME CMD...: runs CMD in the LXD instance NAME, stdin passed through.
on() {
  local name=$1; shift
  lxc exec "$name" -- "$@"
}

# ensure_snap NAME: installs a snap unless it is installed.
ensure_snap() {
  if ! snap list "$1" >/dev/null 2>&1; then
    log "installing the $1 snap"
    snap install "$1"
  fi
}

# ensure_lxd_init PRESEED: initializes LXD unless its default storage pool
# exists.
ensure_lxd_init() {
  if ! lxc storage show default >/dev/null 2>&1; then
    log "initializing LXD"
    printf '%s\n' "$1" | lxd init --preseed
  fi
}

# ensure_instance NAME IMAGE [launch options]: launches an instance unless
# it exists, then waits until it has booted.
ensure_instance() {
  local name=$1 image=$2; shift 2
  if ! lxc info "$name" >/dev/null 2>&1; then
    log "launching $name"
    lxc launch "$image" "$name" "$@"
  fi
  on "$name" cloud-init status --wait >/dev/null
}

# sync_file NAME PATH MODE: makes the file PATH in instance NAME hold stdin;
# returns 0 when it changed it, 1 when it held it already.
sync_file() {
  local name=$1 path=$2 mode=$3 want have
  want=$(cat)
  have=$(on "$name" cat "$path" 2>/dev/null) || have=""
  if [[ "$have" == "$want" ]]; then
    return 1
  fi
  log "writing $path in $name"
  printf '%s\n' "$want" | lxc file push --create-dirs --mode "$mode" - "$name$path"
}

# ensure_packages NAME PKG...: installs the packages an instance lacks.
ensure_packages() {
  local name=$1; shift
  local missing=() p
  for p in "$@"; do
    # shellcheck disable=SC2016 # ${Status} is dpkg-query's, not the shell's
    if [[ $(on "$name" dpkg-query -W -f='${Status}' "$p" 2>/dev/null) != "install ok installed" ]]; then
      missing+=("$p")
    fi
  done
  if (( ${#missing[@]} > 0 )); then
    log "installing ${missing[*]} in $name"
    on "$name" apt-get install -y "${missing[@]}" >/dev/null
  fi
}

# ensure_unit NAME UNIT: enables and starts a systemd unit unless it is.
ensure_unit() {
  local name=$1 unit=$2
  if ! on "$name" systemctl is-enabled --quiet "$unit" || ! on "$name" systemctl is-active --quiet "$unit"; then
    log "starting $unit in $name"
    on "$name" systemctl enable --now "$unit"
  fi
}

# wait_ready NAME URL: waits until the server answers ready.
wait_ready() {
  local name=$1 url=$2 i state
  for (( i = 0; i < ${SEED_WAIT_TRIES:-90}; i++ )); do
    state=$(on "$name" curl -fsS "$url/api/v1/bootstrap/status" 2>/dev/null | jq -r '.state // empty' 2>/dev/null) || state=""
    if [[ "$state" == ready ]]; then
      return 0
    fi
    sleep "${SEED_WAIT_SECONDS:-2}"
  done
  die "the server in $name did not become ready"
}
