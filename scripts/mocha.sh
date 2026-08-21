#!/bin/sh

# This script starts a consensus node on Mocha and state syncs to the tip of the
# chain. Can be sourced by other scripts to reuse setup logic.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

CHAIN_ID="mocha-5"
NODE_NAME="node-name"
SEEDS="ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@84.32.215.148:26656,b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656"
PEERS="daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656"
RPC="https://rpc-mocha.pops.one:443"

CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)

# Setup Mocha state sync configuration
# Sets global variables: LATEST_HEIGHT, BLOCK_HEIGHT, TRUST_HASH
setup_mocha_sync() {
    echo "celestia-app home: ${CELESTIA_APP_HOME}"
    echo "celestia-app version: ${CELESTIA_APP_VERSION}"
    echo ""

    # Ask the user for confirmation before deleting the existing celestia-app home directory
    read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response

    if [ "$response" != "y" ]; then
        echo "You must delete $CELESTIA_APP_HOME to continue."
        exit 1
    fi

    echo "Deleting $CELESTIA_APP_HOME..."
    rm -r "$CELESTIA_APP_HOME"

    echo "Initializing config files..."
    celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Setting seeds in config.toml..."
    sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" $CELESTIA_APP_HOME/config/config.toml

    echo "Setting persistent peers in config.toml..."
    sed -i -e "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"$PEERS\"/;}" $CELESTIA_APP_HOME/config/config.toml

    echo "Querying network for latest height..."
    LATEST_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height);
    BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000));
    TRUST_HASH=$(curl -s "$RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

    echo "Latest height: $LATEST_HEIGHT"
    echo "Block height: $BLOCK_HEIGHT"
    echo "Trust hash: $TRUST_HASH"

    echo "Enabling state sync in config.toml..."
    sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" $HOME/.celestia-app/config/config.toml

    echo "Downloading genesis file..."
    celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise
}

# Only run if executed directly (not sourced)
if [ "${0##*/}" = "mocha.sh" ]; then
    setup_mocha_sync

    echo "Starting celestia-appd..."
    celestia-appd start --force-no-bbr
fi
