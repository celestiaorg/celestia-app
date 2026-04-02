#!/bin/sh

# This script submits a single Fibre blob to the single node testnet.
# Prerequisite: Run ./scripts/single-node-fibre.sh first.
#
# The script uses `go run` to execute the submit-fibre-blob tool, so Go must be installed.
# It will submit a single random blob with a random namespace.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

# Constants
CHAIN_ID="test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
GRPC_ADDR="localhost:9090"
DEPOSIT_AMOUNT="1000000utia"
FEES="5000utia"

VERSION=$(celestia-appd version 2>&1)
APP_HOME="${HOME}/.celestia-app"
GENESIS_FILE="${APP_HOME}/config/genesis.json"

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Get the project root (assuming script is in scripts/ directory)
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "celestia-app version: ${VERSION}"
echo "celestia-app home: ${APP_HOME}"
echo "celestia-app genesis file: ${GENESIS_FILE}"
echo "gRPC address: ${GRPC_ADDR}"
echo ""

# Check if celestia-appd binary exists
if ! command -v celestia-appd >/dev/null 2>&1; then
    echo "Error: celestia-appd binary not found in PATH"
    echo "Please install it using 'make install' or ensure it's in your PATH"
    exit 1
fi

# Check if Go is installed
if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go is not installed or not in PATH"
    echo "Please install Go to run the submit-fibre-blob tool"
    exit 1
fi

# Check if the tool directory exists
if [ ! -d "${PROJECT_ROOT}/tools/submit-fibre-blob" ]; then
    echo "Error: submit-fibre-blob tool directory not found at ${PROJECT_ROOT}/tools/submit-fibre-blob"
    exit 1
fi

# Check if main.go exists
if [ ! -f "${PROJECT_ROOT}/tools/submit-fibre-blob/main.go" ]; then
    echo "Error: main.go not found in ${PROJECT_ROOT}/tools/submit-fibre-blob"
    exit 1
fi

# Check if node is running by checking if we can connect to gRPC
if ! timeout 2 nc -z $(echo "${GRPC_ADDR}" | cut -d: -f1) $(echo "${GRPC_ADDR}" | cut -d: -f2) 2>/dev/null; then
    echo "Warning: Cannot connect to gRPC server at ${GRPC_ADDR}"
    echo "Make sure the node is running (start with ./scripts/single-node-fibre.sh)"
fi

# Check if key exists
if ! celestia-appd keys show "${KEY_NAME}" --keyring-backend="${KEYRING_BACKEND}" --home "${APP_HOME}" >/dev/null 2>&1; then
    echo "Error: Key '${KEY_NAME}' not found in keyring"
    echo "Make sure you've run ./scripts/single-node-fibre.sh first"
    exit 1
fi

# Deposit to escrow account (creates account if it doesn't exist)
echo "Depositing to escrow account..."
celestia-appd tx fibre deposit-to-escrow "${DEPOSIT_AMOUNT}" \
    --from "${KEY_NAME}" \
    --keyring-backend="${KEYRING_BACKEND}" \
    --home "${APP_HOME}" \
    --chain-id "${CHAIN_ID}" \
    --fees "${FEES}" \
    --yes
# Wait for transaction to be included
sleep 3

# Submit blob
echo "Submitting Fibre blob..."
if (cd "${PROJECT_ROOT}" && go run ./tools/submit-fibre-blob \
    --chain-id "${CHAIN_ID}" \
    --key-name "${KEY_NAME}" \
    --keyring-backend "${KEYRING_BACKEND}" \
    --home "${APP_HOME}" \
    --grpc-addr "${GRPC_ADDR}"); then
    echo "Blob submitted successfully"
    exit 0
else
    echo "Blob submission failed"
    exit 1
fi
