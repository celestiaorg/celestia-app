#!/usr/bin/env bash
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Keep the same defaults used by the existing home-level launcher.
export CELESTIA_APPD_BIN="${CELESTIA_APPD_BIN:-${REPO_DIR}/build/celestia-appd}"
export PATH="/home/mikers/go1.23.4/bin:${PATH}"
export DB_BACKEND="${DB_BACKEND:-treedb}"
export APP_DB_BACKEND="${APP_DB_BACKEND:-${DB_BACKEND}}"
export TREEDB_OPEN_PROFILE="${TREEDB_OPEN_PROFILE:-wal_on_fast}"
export TREEDB_FORCE_CHECKPOINT_ON_WRITE="${TREEDB_FORCE_CHECKPOINT_ON_WRITE:-0}"
# TreeDB no longer guarantees a stable "mode=" field in the open banner. Leave
# this empty unless you are intentionally testing a build that logs mode=.
export TREEDB_REQUIRED_OUTER_LEAF_MODE="${TREEDB_REQUIRED_OUTER_LEAF_MODE:-}"

# Optional local-module override for gomap to ensure celestia-appd is built
# against the active local TreeDB branch under development.
USE_LOCAL_GOMAP="${USE_LOCAL_GOMAP:-1}"
LOCAL_GOMAP_DIR="${LOCAL_GOMAP_DIR:-/home/mikers/dev/snissn/gomap-gemini}"
LOCAL_COSMOS_DB_DIR="${LOCAL_COSMOS_DB_DIR:-/home/mikers/dev/snissn/cosmos-db}"
LOCAL_COMET_DB_DIR="${LOCAL_COMET_DB_DIR:-/home/mikers/dev/snissn/cometbft-db}"
LOCAL_COSMOS_STORE_DIR="${LOCAL_COSMOS_STORE_DIR:-/home/mikers/dev/snissn/celestia-cosmos-sdk/store}"
LOCAL_COSMOS_LOG_DIR="${LOCAL_COSMOS_LOG_DIR:-/home/mikers/dev/snissn/celestia-cosmos-sdk/log}"
LOCAL_COSMOS_CORE_DIR="${LOCAL_COSMOS_CORE_DIR:-/home/mikers/dev/snissn/celestia-cosmos-sdk/core}"
LOCAL_IAVL_DIR="${LOCAL_IAVL_DIR:-/home/mikers/dev/snissn/iavl}"
USE_LOCAL_IAVL="${USE_LOCAL_IAVL:-0}"
USE_LOCAL_COSMOS_STORE="${USE_LOCAL_COSMOS_STORE:-1}"

# Preferred mode for local forensics: use full local TreeDB stack
# (gomap + cosmos-db + cometbft-db) via a temporary absolute go.work.
USE_LOCAL_TREE_STACK="${USE_LOCAL_TREE_STACK:-1}"

printf '%s\n' "$(date)"
echo "[run_celestia] Building celestia-appd..."
(
  set -eu
  cd "${REPO_DIR}"

  if [ "${USE_LOCAL_TREE_STACK}" = "1" ]; then
    if [ ! -d "${LOCAL_GOMAP_DIR}" ] || [ ! -f "${LOCAL_GOMAP_DIR}/go.mod" ]; then
      echo "[run_celestia] ERROR: USE_LOCAL_TREE_STACK=1 but LOCAL_GOMAP_DIR is invalid: ${LOCAL_GOMAP_DIR}" >&2
      exit 1
    fi
    if [ ! -d "${LOCAL_COSMOS_DB_DIR}" ] || [ ! -f "${LOCAL_COSMOS_DB_DIR}/go.mod" ]; then
      echo "[run_celestia] ERROR: USE_LOCAL_TREE_STACK=1 but LOCAL_COSMOS_DB_DIR is invalid: ${LOCAL_COSMOS_DB_DIR}" >&2
      exit 1
    fi
    if [ ! -d "${LOCAL_COMET_DB_DIR}" ] || [ ! -f "${LOCAL_COMET_DB_DIR}/go.mod" ]; then
      echo "[run_celestia] ERROR: USE_LOCAL_TREE_STACK=1 but LOCAL_COMET_DB_DIR is invalid: ${LOCAL_COMET_DB_DIR}" >&2
      exit 1
    fi
    if [ "${USE_LOCAL_COSMOS_STORE}" = "1" ]; then
      if [ ! -d "${LOCAL_COSMOS_STORE_DIR}" ] || [ ! -f "${LOCAL_COSMOS_STORE_DIR}/go.mod" ]; then
        echo "[run_celestia] ERROR: USE_LOCAL_COSMOS_STORE=1 but LOCAL_COSMOS_STORE_DIR is invalid: ${LOCAL_COSMOS_STORE_DIR}" >&2
        exit 1
      fi
      if [ ! -d "${LOCAL_COSMOS_LOG_DIR}" ] || [ ! -f "${LOCAL_COSMOS_LOG_DIR}/go.mod" ]; then
        echo "[run_celestia] ERROR: USE_LOCAL_COSMOS_STORE=1 but LOCAL_COSMOS_LOG_DIR is invalid: ${LOCAL_COSMOS_LOG_DIR}" >&2
        exit 1
      fi
      if [ ! -d "${LOCAL_COSMOS_CORE_DIR}" ] || [ ! -f "${LOCAL_COSMOS_CORE_DIR}/go.mod" ]; then
        echo "[run_celestia] ERROR: USE_LOCAL_COSMOS_STORE=1 but LOCAL_COSMOS_CORE_DIR is invalid: ${LOCAL_COSMOS_CORE_DIR}" >&2
        exit 1
      fi
    fi
    if [ "${USE_LOCAL_IAVL}" = "1" ]; then
      if [ ! -d "${LOCAL_IAVL_DIR}" ] || [ ! -f "${LOCAL_IAVL_DIR}/go.mod" ]; then
        echo "[run_celestia] ERROR: USE_LOCAL_IAVL=1 but LOCAL_IAVL_DIR is invalid: ${LOCAL_IAVL_DIR}" >&2
        exit 1
      fi
    fi

    tmp_work="$(mktemp /tmp/run_celestia.XXXXXX.work)"
    cleanup() { rm -f "${tmp_work}"; }
    trap cleanup EXIT

    cat > "${tmp_work}" <<EOF
go 1.25.7

use (
  ${REPO_DIR}
  ${LOCAL_GOMAP_DIR}
  ${LOCAL_COSMOS_DB_DIR}
  ${LOCAL_COMET_DB_DIR}
EOF
    if [ "${USE_LOCAL_COSMOS_STORE}" = "1" ]; then
      cat >> "${tmp_work}" <<EOF
  ${LOCAL_COSMOS_STORE_DIR}
EOF
    fi
    cat >> "${tmp_work}" <<EOF
)

EOF
    if [ "${USE_LOCAL_IAVL}" = "1" ]; then
      cat >> "${tmp_work}" <<EOF
replace github.com/cosmos/iavl => ${LOCAL_IAVL_DIR}
EOF
    fi
    if [ "${USE_LOCAL_COSMOS_STORE}" = "1" ]; then
      cat >> "${tmp_work}" <<EOF
replace cosmossdk.io/log => ${LOCAL_COSMOS_LOG_DIR}
replace cosmossdk.io/core => ${LOCAL_COSMOS_CORE_DIR}
EOF
    fi
    PATH="/home/mikers/go1.23.4/bin:${PATH}" \
      GOWORK="${tmp_work}" \
      GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.7}" \
      go build -o build/celestia-appd ./cmd/celestia-appd
  elif [ "${USE_LOCAL_GOMAP}" = "1" ]; then
    if [ ! -d "${LOCAL_GOMAP_DIR}" ] || [ ! -f "${LOCAL_GOMAP_DIR}/go.mod" ]; then
      echo "[run_celestia] ERROR: USE_LOCAL_GOMAP=1 but LOCAL_GOMAP_DIR is invalid: ${LOCAL_GOMAP_DIR}" >&2
      exit 1
    fi

    tmp_mod="$(mktemp "${REPO_DIR}/.run_celestia.mod.XXXXXX.mod")"
    tmp_sum="${tmp_mod%.mod}.sum"
    cleanup() { rm -f "${tmp_mod}" "${tmp_sum}"; }
    trap cleanup EXIT

    cp go.mod "${tmp_mod}"
    cp go.sum "${tmp_sum}"
    {
      echo ""
      echo "replace github.com/snissn/gomap => ${LOCAL_GOMAP_DIR}"
    } >> "${tmp_mod}"

    PATH="/home/mikers/go1.23.4/bin:${PATH}" \
      GOWORK=off \
      GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.7}" \
      go build -modfile="${tmp_mod}" -o build/celestia-appd ./cmd/celestia-appd
  else
    PATH="/home/mikers/go1.23.4/bin:${PATH}" \
      GOWORK=off \
      GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.7}" \
      go build -o build/celestia-appd ./cmd/celestia-appd
  fi
)

echo "[run_celestia] Build info (selected modules):"
go version -m "${CELESTIA_APPD_BIN}" | rg -n "github.com/snissn/gomap|github.com/cosmos/cosmos-db|github.com/cometbft/cometbft-db|github.com/cosmos/iavl|=>"

if [ "${USE_LOCAL_TREE_STACK}" = "1" ]; then
  if ! go version -m "${CELESTIA_APPD_BIN}" | grep -Fq $'dep\tgithub.com/snissn/gomap\t(devel)'; then
    echo "[run_celestia] ERROR: local gomap workspace override not active." >&2
    exit 1
  fi
  if ! go version -m "${CELESTIA_APPD_BIN}" | grep -Fq $'dep\tgithub.com/cosmos/cosmos-db\t(devel)'; then
    echo "[run_celestia] ERROR: local cosmos-db workspace override not active." >&2
    exit 1
  fi
  if ! go version -m "${CELESTIA_APPD_BIN}" | grep -Fq $'dep\tgithub.com/cometbft/cometbft-db\t(devel)'; then
    echo "[run_celestia] ERROR: local cometbft-db workspace override not active." >&2
    exit 1
  fi
  if [ "${USE_LOCAL_COSMOS_STORE}" = "1" ]; then
    if ! go version -m "${CELESTIA_APPD_BIN}" | grep -Fq $'dep\tcosmossdk.io/store\t(devel)'; then
      echo "[run_celestia] ERROR: local cosmossdk.io/store workspace override not active." >&2
      exit 1
    fi
  fi
  if [ "${USE_LOCAL_IAVL}" = "1" ]; then
    if ! go version -m "${CELESTIA_APPD_BIN}" | grep -Fq "${LOCAL_IAVL_DIR}"; then
      echo "[run_celestia] ERROR: local iavl override not active in build info." >&2
      exit 1
    fi
  fi
elif [ "${USE_LOCAL_GOMAP}" = "1" ]; then
  if ! go version -m "${CELESTIA_APPD_BIN}" | grep -Fq "github.com/snissn/gomap => ${LOCAL_GOMAP_DIR}"; then
    echo "[run_celestia] ERROR: local gomap override not active in build info." >&2
    exit 1
  fi
fi

if [ "${APP_DB_BACKEND}" = "treedb" ]; then
  if [ ! -d "${LOCAL_GOMAP_DIR}" ] || [ ! -f "${LOCAL_GOMAP_DIR}/go.mod" ]; then
    echo "[run_celestia] ERROR: APP_DB_BACKEND=treedb requires LOCAL_GOMAP_DIR to build treemap: ${LOCAL_GOMAP_DIR}" >&2
    exit 1
  fi
  TREEMAP_BIN="${TREEMAP_BIN:-${REPO_DIR}/build/treemap-local}"
  echo "[run_celestia] Building treemap from local gomap: ${LOCAL_GOMAP_DIR}"
  (
    set -eu
    cd "${LOCAL_GOMAP_DIR}"
    treemap_pkg="./TreeDB/cmd/treemap"
    if [ ! -d "${LOCAL_GOMAP_DIR}/TreeDB/cmd/treemap" ] && [ -d "${LOCAL_GOMAP_DIR}/cmd/treemap" ]; then
      treemap_pkg="./cmd/treemap"
    fi
    PATH="/home/mikers/go1.23.4/bin:${PATH}" \
      GOWORK=off \
      GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.7}" \
      go build -o "${TREEMAP_BIN}" "${treemap_pkg}"
  )
  export TREEMAP_BIN
  echo "[run_celestia] treemap binary: ${TREEMAP_BIN}"
  go version -m "${TREEMAP_BIN}" | rg -n "github.com/snissn/gomap|=>"
fi

echo "[run_celestia] Starting monitored sync..."
exec "${REPO_DIR}/scripts/mainnet-treedb-fast-sync-forensics.sh" "$@"
