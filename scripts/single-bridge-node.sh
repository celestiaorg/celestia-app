#!/bin/sh

# This script starts a bridge node that is connected to a celestia-app validator started via scripts/single-node.sh.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

if ! [ -x "$(command -v celestia)" ]
then
    echo "celestia could not be found. Please install the celestia binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

CHAIN_ID="test"
CELESTIA_HOME="${HOME}/.celestia-bridge-${CHAIN_ID}"
VERSION=$(celestia version 2>&1)
GENESIS_BLOCK_HASH=$(curl http://localhost:26657/block?height=1 | jq -r .result.block_id.hash)
CELESTIA_CUSTOM="${CHAIN_ID}:${GENESIS_BLOCK_HASH}"

echo "celestia version: ${VERSION}"
echo "celestia home: ${CELESTIA_HOME}"
echo "Genesis block hash: ${GENESIS_BLOCK_HASH}"
echo "CELESTIA_CUSTOM: ${CELESTIA_CUSTOM}"
echo ""

# Set the CELESTIA_CUSTOM environment variable. Private networks must be
# configured with env variables instead of via CLI flags (e.g. --p2p.network).
export CELESTIA_CUSTOM=$CELESTIA_CUSTOM

createConfig() {
    echo "Initializing bridge node config files..."
    celestia bridge init --core.ip 127.0.0.1
    echo "Initialized bridge node config files."
}

deleteCelestiaHome() {
    echo "Deleting $CELESTIA_HOME..."
    rm -rf "$CELESTIA_HOME"
    echo "Deleted $CELESTIA_HOME."
}

startCelestia() {
  echo "Starting celestia bridge node..."
  celestia bridge start --core.ip 127.0.0.1 --log.level debug
}

deleteCelestiaHome
createConfig
startCelestia
