#!/bin/sh

# This script starts a consensus node on Mocha and state syncs to the tip of the
# chain.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

CHAIN_ID="mocha-4"
NODE_NAME="node-name"
SEEDS="b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656,ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@seed-mocha.pops.one:26656"
PEERS="fb5e0b9efacc11916c58bbcd3606cbaa7d43c99f@65.108.234.84:28656,7a649733c5ae1b8bba9a5d855d697811646a0f6a@184.107.149.93:36656,7acb49ef77a268b8ae134ad9db3632c933e5013a@212.83.43.40:26656,f9e950870eccdb40e2386896d7b6a7687a103c99@72.251.3.24:43656,2666d40498d8d435d7b29af7d157b64bcc37a39a@162.249.168.87:26656,ad95eb726622347266baf0913b065103a8f9d6ff@159.195.26.108:23656"
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

echo "Setting ttl-num-blocks to 1\ in config.toml..."
sed -i.bak -e "s/^ttl-num-blocks *=.*/ttl-num-blocks = 1/" $CELESTIA_APP_HOME/config/config.toml

LATEST_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height);
BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000)); \
TRUST_HASH=$(curl -s "$RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

echo "Latest height: $LATEST_HEIGHT"
echo "Block height: $BLOCK_HEIGHT"
echo "Trust hash: $TRUST_HASH"
echo "Enabling state sync and gRPC in config.toml..."
sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"| ; \
s|^(grpc_laddr[[:space:]]+=[[:space:]]+).*$|\1\"tcp://0.0.0.0:9098\"|" $HOME/.celestia-app/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

echo "Starting celestia-appd..."
celestia-appd start --force-no-bbr --p2p.persistent_peers=${PEERS} --grpc.enable --grpc.address="0.0.0.0:9090"
