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

CELESTIA_HOME="${HOME}/.celestia-bridge-private"
VERSION=$(celestia version 2>&1)

echo "celestia version: ${VERSION}"
echo "celestia home: ${CELESTIA_HOME}"
echo ""

createConfig() {
    echo "Initializing bridge node config files..."
    celestia bridge init \
        --p2p.network private \
        --core.ip 127.0.0.1 \
        --core.port 9098
    #   > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Initialized bridge node config files"
}

deleteCelestiaHome() {
    echo "Deleting $CELESTIA_HOME..."
    rm -rf "$CELESTIA_HOME"
}

startCelestia() {
  echo "Starting celestia..."
  celestia bridge start \
    --p2p.network private \
    --core.ip 127.0.0.1 \
    --core.port 9098
}

deleteCelestiaHome
createConfig
startCelestia
