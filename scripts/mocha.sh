#!/bin/sh

# This script starts a consensus node on Mocha and state syncs to the tip of the
# chain.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

CHAIN_ID="mocha-4"
NODE_NAME="node-name"
SEEDS="b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656"
PEERS="daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656,96b2761729cea90ee7c61206433fc0ba40c245bf@57.128.141.126:11656,f4f75a55bfc5f302ef34435ef096a4551ecb6804@152.53.33.96:12056,31bb1c9c1be7743d1115a8270bd1c83d01a9120a@148.72.141.31:26676,3e30bcfc55e7d351f18144aab4b0973e9e9bf987@65.108.226.183:11656,7a0d5818c0e5b0d4fbd86a9921f413f5e4e4ac1e@65.109.83.40:28656,43e9da043318a4ea0141259c17fcb06ecff816af@164.132.247.253:43656,5a7566aa030f7e5e7114dc9764f944b2b1324bcd@65.109.23.114:11656,c17c0cbf05e98656fee5f60fad469fc528f6d6de@65.109.25.113:11656,fb5e0b9efacc11916c58bbcd3606cbaa7d43c99f@65.108.234.84:28656,45504fb31eb97ea8778c920701fc8076e568a9cd@188.214.133.100:26656,edafdf47c443344fb940a32ab9d2067c482e59df@84.32.71.47:26656,ae7d00d6d70d9b9118c31ac0913e0808f2613a75@177.54.156.69:26656,7c841f59c35d70d9f1472d7d2a76a11eefb7f51f@136.243.69.100:43656"
RPC="https://celestia-testnet-rpc.itrocket.net:443"

CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""

# Ask the user for confirmation before deleting the existing celestia-app home
# directory.
read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response

# Check the user's response
if [ "$response" != "y" ]; then
    # Exit if the user did not respond with "y"
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

LATEST_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height);
BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000)); \
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

echo "Starting celestia-appd..."
celestia-appd start --force-no-bbr
