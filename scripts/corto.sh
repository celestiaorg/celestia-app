#!/bin/bash

# This script starts a consensus node on Corto and state syncs to the tip of
# the chain. It expects celestia-appd to be installed.
#
# Environment variables:
#   CORTO_RPC        (required) RPC endpoint, e.g. https://rpc.celestia-corto.com:443
#   CORTO_AUTH_TOKEN (required) auth token when the RPC sits behind an auth proxy
#
# Usage: ./scripts/corto.sh [app-home]

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

CHAIN_ID="corto-1"
NODE_NAME="node-name"

CORTO_RPC="${CORTO_RPC:?must be set, e.g. https://rpc.celestia-corto.com:443}"
CORTO_AUTH_TOKEN="${CORTO_AUTH_TOKEN:-}"

# Use argument as home directory if provided, else default to ~/.celestia-app
CELESTIA_APP_HOME="${1:-${HOME}/.celestia-app}"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)

# The proxy in front of Corto accepts the token as a Bearer header (used for
# curl) and as a basic-auth password (used for state sync rpc_servers, because
# CometBFT can only pass credentials via the URL).
RPC_SERVER="$CORTO_RPC"
if [ -n "$CORTO_AUTH_TOKEN" ]; then
    RPC_SERVER="${CORTO_RPC/:\/\//:\/\/token:${CORTO_AUTH_TOKEN}@}"
fi

rpc_get() {
    if [ -n "$CORTO_AUTH_TOKEN" ]; then
        curl -s -H "Authorization: Bearer $CORTO_AUTH_TOKEN" "$1"
    else
        curl -s "$1"
    fi
}

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""

if [ -d "$CELESTIA_APP_HOME" ]; then
    read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response
    if [ "$response" != "y" ]; then
        echo "You must delete $CELESTIA_APP_HOME to continue."
        exit 1
    fi
    echo "Deleting $CELESTIA_APP_HOME..."
    rm -r "$CELESTIA_APP_HOME"
fi

echo "Initializing config files..."
celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} --home "$CELESTIA_APP_HOME" > /dev/null 2>&1 # Hide output to reduce terminal noise

echo "Querying $CORTO_RPC for peers..."
PEERS=$(rpc_get "$CORTO_RPC/net_info" | jq -r '[.result.peers[] | "\(.node_info.id)@\(.remote_ip):\(.node_info.listen_addr | split(":") | last)"] | join(",")')
echo "Peers: $PEERS"

echo "Setting persistent peers in config.toml..."
sed -i.bak -e "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"$PEERS\"/;}" "$CELESTIA_APP_HOME/config/config.toml"

echo "Querying network for latest height..."
LATEST_HEIGHT=$(rpc_get "$CORTO_RPC/block" | jq -r .result.block.header.height)
BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000))
TRUST_HASH=$(rpc_get "$CORTO_RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

echo "Latest height: $LATEST_HEIGHT"
echo "Block height: $BLOCK_HEIGHT"
echo "Trust hash: $TRUST_HASH"

echo "Enabling state sync in config.toml..."
sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC_SERVER,$RPC_SERVER\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" "$CELESTIA_APP_HOME/config/config.toml"

echo "Downloading genesis file..."
# Older binaries (e.g. v9.0.6-corto) don't support download-genesis for
# corto-1, so fall back to fetching the genesis file directly.
if ! celestia-appd download-genesis ${CHAIN_ID} --home "$CELESTIA_APP_HOME"; then
    echo "download-genesis failed, downloading genesis with curl..."
    curl -sf "https://raw.githubusercontent.com/celestiaorg/networks/master/${CHAIN_ID}/genesis.json" -o "$CELESTIA_APP_HOME/config/genesis.json"
fi

echo "Starting celestia-appd..."
celestia-appd start --home "$CELESTIA_APP_HOME" --force-no-bbr
