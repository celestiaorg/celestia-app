#!/bin/bash

# Entrypoint script for an Arabica consensus node with state sync.
# This script is idempotent: on restart it skips init/config and goes straight
# to starting the node.

set -o errexit
set -o nounset
set -o pipefail

CHAIN_ID="arabica-11"
NODE_NAME="arabica-node"
SEEDS="ae7a366fd79f598b8135f9edef0f2cf875a3d3ba@validator-1.celestia-arabica-11.com:26656,4e38c37e88dcbf7809f6636120a7226621a684dd@validator-2.celestia-arabica-11.com:26656,365b07692fdc8dd996063f970cf6788ef608ddb1@validator-3.celestia-arabica-11.com:26656,4a52f45cf5518aa776f8d7462ab2e593fceb7154@validator-4.celestia-arabica-11.com:26656"
RPC="https://rpc.celestia-arabica-11.com:443"

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
