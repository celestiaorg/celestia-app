#!/bin/sh

# This script builds the celestia-app repo and attempts to connect to arabica-10.
# This script assumes that it is executed from the root of the celestia-app repo.
# ./scripts/arabica-10

set -o errexit -o nounset

CELESTIA_APP_HOME="$HOME/.celestia-app"
NETWORKS_PATH="$HOME/git/rootulp/celestia/networks"
BINARY_PATH="./build/celestia-appd"
CHAIN_ID="arabica-10"
NODE_NAME="node-name"

# echo "Building celestia-appd"
# make build

echo "Deleting existing celestia-app home"
rm -rf ${CELESTIA_APP_HOME}

echo "Initializing new celestia-app home"
# redirect the output to /dev/null to avoid polluting the terminal output
${BINARY_PATH} init ${NODE_NAME} --chain-id ${CHAIN_ID} --home ${CELESTIA_APP_HOME} &> /dev/null

echo "Copying genesis.json from networks repo to celestia-app config"
cp ${NETWORKS_PATH}/${CHAIN_ID}/genesis.json ${CELESTIA_APP_HOME}/config

echo "Copying addrbook.json to celestia-app config"
cp /Users/rootulp/git/rootulp/celestia/celestia-app/tools/addrbook/addrbook.json ${CELESTIA_APP_HOME}/config/

echo "Getting persistent peers from networks repo"
PERSISTENT_PEERS=$(curl -X GET "https://raw.githubusercontent.com/celestiaorg/networks/master/${CHAIN_ID}/peers.txt" | tr '\n' ',')

echo "Setting persistent peers to ${PERSISTENT_PEERS}"
sed -i.bak -e "s/^persistent_peers *=.*/persistent_peers = \"$PERSISTENT_PEERS\"/" ${CELESTIA_APP_HOME}/config/config.toml

echo "Starting celestia-appd"
${BINARY_PATH} start --home ${CELESTIA_APP_HOME} --api.enable
