#!/bin/sh

# This script builds the celestia-app repo and attempts to connect to mocha-4.
# This script assumes that it is executed from the root of the celestia-app repo.
# ./scripts/mocha-4.sh

set -o errexit -o nounset

CELESTIA_APP_HOME="$HOME/.celestia-app"
NETWORKS_PATH="$HOME/git/rootulp/celestia/networks"
BINARY_PATH="./build/celestia-appd"
CHAIN_ID="mocha-4"
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

echo "Setting seeds"
SEEDS="3314051954fc072a0678ec0cbac690ad8676ab98@65.108.66.220:26656,258f523c96efde50d5fe0a9faeea8a3e83be22ca@seed.mocha-4.celestia.aviaone.com:20279,5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656,7da0fb48d6ef0823bc9770c0c8068dd7c89ed4ee@celest-test-seed.theamsolutions.info:443"
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" ${CELESTIA_APP_HOME}/config/config.toml

echo "Setting persistent peers"
PERSISTENT_PEERS="34499b1ac473fbb03894c883178ecc83f0d6eaf6@64.227.18.169:26656,43e9da043318a4ea0141259c17fcb06ecff816af@rpc-1.celestia.nodes.guru:43656,f9e950870eccdb40e2386896d7b6a7687a103c99@rpc-2.celestia.nodes.guru:43656,daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656,f0c7ef0af1c3557dc05509ba6dff2a22bdc705e9@65.108.238.61:13656"
sed -i.bak -e "s/^persistent_peers *=.*/persistent_peers = \"$PERSISTENT_PEERS\"/" ${CELESTIA_APP_HOME}/config/config.toml

echo "Starting celestia-appd"
${BINARY_PATH} start --home ${CELESTIA_APP_HOME} --api.enable
