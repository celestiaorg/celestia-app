#!/bin/sh

# This script starts a bridge node that is connected to a local celestia-app validator.
# Prerequisites:
# 1. Run ./scripts/single-node.sh in another terminal window.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

if ! [ -x "$(command -v celestia)" ]
then
    echo "celestia could not be found. Please install the celestia binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

CHAIN_ID="test"
CELESTIA_HOME="${HOME}/.celestia-bridge-${CHAIN_ID}"
VERSION=$(celestia version 2>&1)
CORE_IP="127.0.0.1"

echo "Waiting for celestia-app to start..."
while true; do
    GENESIS_BLOCK_HASH=$(curl -s http://localhost:26657/block?height=1 2>/dev/null | jq -r '.result.block_id.hash')
    if [ -n "$GENESIS_BLOCK_HASH" ] && [ "$GENESIS_BLOCK_HASH" != "null" ]; then
        echo "Genesis block hash: $GENESIS_BLOCK_HASH"
        break
    fi
    echo "Waiting for genesis block hash..."
    sleep 1
done

echo "Found genesis block hash: $GENESIS_BLOCK_HASH"
GENESIS_BLOCK_HASH=$(curl -s http://localhost:26657/block?height=1 | jq -r .result.block_id.hash)
CELESTIA_CUSTOM="${CHAIN_ID}:${GENESIS_BLOCK_HASH}"

echo "celestia version: ${VERSION}"
echo "celestia home: ${CELESTIA_HOME}"
echo "Genesis block hash: ${GENESIS_BLOCK_HASH}"
echo "CELESTIA_CUSTOM: ${CELESTIA_CUSTOM}"
echo ""

# Set the CELESTIA_CUSTOM environment variable. Private networks must be
# configured with env variables instead of via CLI flags (e.g. --p2p.network).
export CELESTIA_CUSTOM=$CELESTIA_CUSTOM

deleteCelestiaHome() {
    echo "Deleting $CELESTIA_HOME..."
    rm -rf "$CELESTIA_HOME"
    echo "Deleted $CELESTIA_HOME."
}

createConfig() {
    echo "Initializing bridge node config files..."
    celestia bridge init --core.ip $CORE_IP
    echo "Initialized bridge node config files."
}

startCelestia() {
    echo "Waiting for 6 seconds before starting bridge node..."
    sleep 6
    echo "Starting celestia bridge node..."
    celestia bridge start --core.ip $CORE_IP
}

deleteCelestiaHome
createConfig
startCelestia
