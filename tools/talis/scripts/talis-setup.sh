#!/usr/bin/env bash
set -euo pipefail

# Configurable parameters (override via env or flags)
NETWORK_NAME="${NETWORK_NAME:-test}"
NUM_VALIDATORS="${NUM_VALIDATORS:-4}"

# Required environment variables
REQUIRED_VARS=(
  DIGITALOCEAN_TOKEN
  TALIS_SSH_KEY_PATH
  TALIS_SSH_PUB_KEY_PATH
  AWS_ACCESS_KEY_ID
  AWS_SECRET_ACCESS_KEY
  AWS_S3_BUCKET
  AWS_S3_ENDPOINT
  AWS_DEFAULT_REGION
)

missing=()
for var in "${REQUIRED_VARS[@]}"; do
  if [[ -z "${!var:-}" ]]; then
    missing+=("$var")
  fi
done

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "ERROR: missing required environment variables:"
  for var in "${missing[@]}"; do
    echo "  - $var"
  done
  exit 1
fi

export COPYFILE_DISABLE=1

# Resolve repo root (script lives in tools/talis/scripts/)
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TALIS_DIR="${REPO_ROOT}/talis-setup"
TALIS_BIN="$(command -v talis)" || { echo "ERROR: talis binary not found in PATH. Run: go install ./tools/talis"; exit 1; }

step_init() {
  echo "==> Initializing talis network '${NETWORK_NAME}' ..."
  "${TALIS_BIN}" init \
    -d "${TALIS_DIR}" \
    -c "${NETWORK_NAME}" \
    -e metrics \
    --with-observability \
    --src-root "${REPO_ROOT}/.."
}

step_add() {
  echo "==> Adding ${NUM_VALIDATORS} validators in sfo2..."
  "${TALIS_BIN}" add \
    -d "${TALIS_DIR}" \
    -t validator \
    -c "${NUM_VALIDATORS}" \
    -r sfo2
}

step_build() {
  local bins=(celestia-appd txsim latency-monitor fibre fibre-txsim talis)
  local need_build=false
  for b in "${bins[@]}"; do
    if [[ ! -f "${TALIS_DIR}/build/${b}" ]]; then
      need_build=true
      break
    fi
  done
  if [[ "${need_build}" == "true" ]]; then
    echo "==> Building talis binaries..."
    make -C "${REPO_ROOT}" build-talis-bins
  else
    echo "==> All binaries already built, skipping (delete talis-setup/build/ to force rebuild)"
  fi
}

step_up() {
  echo "==> Provisioning VMs..."
  "${TALIS_BIN}" up \
    -d "${TALIS_DIR}" \
    -s "${TALIS_SSH_PUB_KEY_PATH}"
}

step_genesis() {
  echo "==> Generating genesis..."
  "${TALIS_BIN}" genesis \
    -d "${TALIS_DIR}" \
    -s 512 \
    -b "${TALIS_DIR}/build" \
    --observability-dir "${REPO_ROOT}/observability"
}

step_deploy() {
  echo "==> Deploying binaries and configs..."
  "${TALIS_BIN}" deploy \
    -d "${TALIS_DIR}"
}

step_setup_fibre() {
  echo "==> Setting up fibre keys and config..."
  "${TALIS_BIN}" setup-fibre \
    -d "${TALIS_DIR}"
}

step_start_fibre() {
  echo "==> Starting fibre servers..."
  "${TALIS_BIN}" start-fibre \
    -d "${TALIS_DIR}"
}

step_txsim() {
  echo "==> Starting fibre txsim load generators..."
  "${TALIS_BIN}" fibre-txsim \
    -d "${TALIS_DIR}" \
    --instances 2 \
    --concurrency 4
}

step_down() {
  echo "==> Tearing down VMs..."
  "${TALIS_BIN}" down \
    -d "${TALIS_DIR}" \
    -s "${TALIS_SSH_PUB_KEY_PATH}"
}

ALL_STEPS=(init add build up genesis deploy setup-fibre start-fibre txsim)

usage() {
  echo "Usage: $0 [-n NETWORK_NAME] [-v NUM_VALIDATORS] [step ...]"
  echo ""
  echo "Options:"
  echo "  -n NAME   Network name (default: exp-fibre, or NETWORK_NAME env)"
  echo "  -v COUNT  Number of validators (default: 4, or NUM_VALIDATORS env)"
  echo ""
  echo "Steps (run all if none specified):"
  echo "  init          Initialize talis network"
  echo "  add           Add validators"
  echo "  build         Build talis binaries (make build-talis-bins)"
  echo "  up            Provision VMs"
  echo "  genesis       Generate genesis and configs"
  echo "  deploy        Deploy binaries and configs"
  echo "  setup-fibre   Setup fibre keys and config"
  echo "  start-fibre   Start fibre servers"
  echo "  txsim         Start fibre txsim load generators"
  echo "  down          Tear down VMs (not included in 'all')"
  echo ""
  echo "Examples:"
  echo "  $0                          # run all steps with defaults"
  echo "  $0 -n my-test -v 8          # 8 validators, network 'my-test'"
  echo "  $0 deploy                   # only deploy"
  echo "  $0 down                     # tear down"
}

run_step() {
  case "$1" in
    init)         step_init ;;
    add)          step_add ;;
    build)        step_build ;;
    up)           step_up ;;
    genesis)      step_genesis ;;
    deploy)       step_deploy ;;
    setup-fibre)  step_setup_fibre ;;
    start-fibre)  step_start_fibre ;;
    txsim)        step_txsim ;;
    down)         step_down ;;
    -h|--help)    usage; exit 0 ;;
    *)            echo "ERROR: unknown step '$1'"; usage; exit 1 ;;
  esac
}

# Parse flags
while getopts "n:v:h" opt; do
  case "$opt" in
    n) NETWORK_NAME="$OPTARG" ;;
    v) NUM_VALIDATORS="$OPTARG" ;;
    h) usage; exit 0 ;;
    *) usage; exit 1 ;;
  esac
done
shift $((OPTIND - 1))

if [[ $# -eq 0 ]]; then
  for step in "${ALL_STEPS[@]}"; do
    run_step "$step"
  done
else
  for step in "$@"; do
    run_step "$step"
  done
fi
