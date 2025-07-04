#!/bin/sh

# Start a consensus node and state sync to the tip on Arabica, Mocha, or Mainnet.
# Usage:
#   ./scripts/state-sync.sh [--network <arabica|mocha|mainnet>]
# Defaults to mainnet if --network is not specified.

set -o errexit
set -o nounset

if ! [ -x "$(command -v celestia-appd)" ]; then
  echo "celestia-appd could not be found. Please install the celestia-appd binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
  exit 1
fi

usage() {
  echo "Usage: $0 [--network <arabica|mocha|mainnet>]"
  exit 1
}

# Parse args
if [ $# -ne 2 ] || [ "$1" != "--network" ]; then
  echo "No network provided, defaulting to mainnet"
  NETWORK="mainnet"
else
  # Convert to lowercase
  NETWORK=$(echo "$2" | tr '[:upper:]' '[:lower:]')
fi

case "$NETWORK" in
arabica)
  CHAIN_ID="arabica-11"
  SEEDS="827583022cc6ce65cf762115642258f937c954cd@validator-1.celestia-arabica-11.com:26656,74e42b39f512f844492ff09e30af23d54579b7bc@validator-2.celestia-arabica-11.com:26656,00d577159b2eb1f524ef9c37cb389c020a2c38d2@validator-3.celestia-arabica-11.com:26656,b2871b6dc2e18916d07264af0e87c456c2bba04f@validator-4.celestia-arabica-11.com:26656"
  RPC="https://rpc.celestia-arabica-11.com:443"
  ;;
mocha)
  CHAIN_ID="mocha-4"
  SEEDS="5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656"
  RPC="https://celestia-testnet-rpc.itrocket.net:443"
  ;;
mainnet)
  CHAIN_ID="celestia"
  SEEDS="e6116822e1a5e283d8a85d3ec38f4d232274eaf3@consensus-full-seed-1.celestia-bootstrap.net:26656,cf7ac8b19ff56a9d47c75551bd4864883d1e24b5@consensus-full-seed-2.celestia-bootstrap.net:26656"
  RPC="https://celestia-rpc.polkachu.com:443"
  ;;
*)
  echo "Unknown network: $NETWORK"
  usage
  ;;
esac

CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo "chain id: ${CHAIN_ID}"
echo ""

# For public networks
NODE_NAME="node-name"

# Ask the user for confirmation before deleting the existing celestia-app home directory.
read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response
if [ "$response" != "y" ]; then
  echo "You must delete $CELESTIA_APP_HOME to continue."
  exit 1
fi
echo "Deleting $CELESTIA_APP_HOME..."
rm -r "$CELESTIA_APP_HOME"

echo "Initializing config files..."
celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} >/dev/null 2>&1 # Hide output to reduce terminal noise

echo "Setting seeds in config.toml..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" $CELESTIA_APP_HOME/config/config.toml

LATEST_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height)
BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000))
TRUST_HASH=$(curl -s "$RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

echo "Block height: $BLOCK_HEIGHT"
echo "Trust hash: $TRUST_HASH"
echo "Enabling state sync in config.toml..."
sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" $HOME/.celestia-app/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} >/dev/null 2>&1 # Hide output to reduce terminal noise

echo "Starting celestia-appd..."
eval celestia-appd start --force-no-bbr
