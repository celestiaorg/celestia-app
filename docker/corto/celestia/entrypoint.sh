#!/bin/bash

# Entrypoint script for a Corto consensus node with state sync.
# This script is idempotent: on restart it skips init/config and goes straight
# to starting the node.
#
# WARNING: SEEDS and RPC are placeholders. They must be set to the internal
# Corto network endpoints before this setup can be used.

set -o errexit
set -o nounset
set -o pipefail

CHAIN_ID="corto-1"
NODE_NAME="corto-node"
SEEDS="TBD"
RPC="TBD"

CELESTIA_APP_HOME="/home/celestia/.celestia-app"

if [ ! -f "${CELESTIA_APP_HOME}/config/config.toml" ]; then
    echo "Initializing node..."
    celestia-appd init "${NODE_NAME}" --chain-id "${CHAIN_ID}" > /dev/null 2>&1

    echo "Downloading genesis file..."
    celestia-appd download-genesis "${CHAIN_ID}" > /dev/null 2>&1

    echo "Fetching latest block height for state sync..."
    LATEST_HEIGHT=$(curl -s "${RPC}/block" | jq -r .result.block.header.height)
    BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000))
    TRUST_HASH=$(curl -s "${RPC}/block?height=${BLOCK_HEIGHT}" | jq -r .result.block_id.hash)

    echo "Latest height: ${LATEST_HEIGHT}"
    echo "Trust height:  ${BLOCK_HEIGHT}"
    echo "Trust hash:    ${TRUST_HASH}"

    echo "Configuring seeds..."
    sed -i "s/^seeds *=.*/seeds = \"${SEEDS}\"/" "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Enabling state sync..."
    sed -i -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
    s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"${RPC},${RPC}\"| ; \
    s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1${BLOCK_HEIGHT}| ; \
    s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"${TRUST_HASH}\"|" "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Enabling Prometheus instrumentation..."
    sed -i '/^\[instrumentation\]/,/^\[/{s/^prometheus *=.*/prometheus = true/;}' "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Node initialized and configured."
else
    echo "Node already initialized, skipping init/config."
fi

echo "Starting celestia-appd..."
exec celestia-appd start --force-no-bbr
