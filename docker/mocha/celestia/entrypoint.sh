#!/bin/bash

# Entrypoint script for a Mocha consensus node using the multiplexer binary.
#
# This script is idempotent: on restart it skips init/config and goes straight
# to starting the node.

set -o errexit
set -o nounset
set -o pipefail

CHAIN_ID="mocha-5"
NODE_NAME="mocha-node"
SEEDS="ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@84.32.215.148:26656,b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656"
PEERS="daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656"
RPC="https://rpc-mocha.pops.one:443"

CELESTIA_APP_HOME="/home/celestia/.celestia-app"

if [ ! -f "${CELESTIA_APP_HOME}/config/config.toml" ]; then
    echo "Initializing node..."
    celestia-appd init "${NODE_NAME}" --chain-id "${CHAIN_ID}" > /dev/null 2>&1

    echo "Downloading genesis file..."
    celestia-appd download-genesis "${CHAIN_ID}" > /dev/null 2>&1

    echo "Configuring seeds..."
    sed -i "s/^seeds *=.*/seeds = \"${SEEDS}\"/" "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Configuring persistent peers..."
    sed -i "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"${PEERS}\"/;}" "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Enabling Prometheus instrumentation..."
    sed -i '/^\[instrumentation\]/,/^\[/{s/^prometheus *=.*/prometheus = true/;}' "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Enabling telemetry in app.toml..."
    sed -i '/^\[telemetry\]/,/^\[/{s/^enabled *=.*/enabled = true/;}' "${CELESTIA_APP_HOME}/config/app.toml"
    sed -i '/^\[telemetry\]/,/^\[/{s/^prometheus-retention-time *=.*/prometheus-retention-time = 60/;}' "${CELESTIA_APP_HOME}/config/app.toml"

    echo "Enabling state sync in config.toml..."
    LATEST_HEIGHT=$(curl -s "${RPC}/block" | jq -r .result.block.header.height)
    BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000))
    TRUST_HASH=$(curl -s "${RPC}/block?height=${BLOCK_HEIGHT}" | jq -r .result.block_id.hash)
    echo "State sync: height=${BLOCK_HEIGHT} hash=${TRUST_HASH}"
    sed -i -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"${RPC},${RPC}\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1${BLOCK_HEIGHT}| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"${TRUST_HASH}\"|" "${CELESTIA_APP_HOME}/config/config.toml"

    echo "Node initialized and configured."
else
    echo "Node already initialized, skipping init/config."
fi

echo "Starting celestia-appd..."
exec celestia-appd start --force-no-bbr
