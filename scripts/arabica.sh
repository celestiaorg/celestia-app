#!/bin/sh

# This script starts a consensus node on Arabica and state syncs to the tip of
# the chain.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

CHAIN_ID="arabica-11"
NODE_NAME="node-name"
SEEDS="827583022cc6ce65cf762115642258f937c954cd@validator-1.celestia-arabica-11.com:26656,74e42b39f512f844492ff09e30af23d54579b7bc@validator-2.celestia-arabica-11.com:26656,00d577159b2eb1f524ef9c37cb389c020a2c38d2@validator-3.celestia-arabica-11.com:26656,b2871b6dc2e18916d07264af0e87c456c2bba04f@validator-4.celestia-arabica-11.com:26656"
RPC="https://rpc.celestia-arabica-11.com:443"

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

echo "Settings seeds in config.toml..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" $CELESTIA_APP_HOME/config/config.toml

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
celestia-appd download-genesis ${CHAIN_ID}

echo "Starting celestia-appd..."
celestia-appd start --v2-upgrade-height 1751707
