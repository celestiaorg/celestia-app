#!/bin/sh

# Start a consensus node and block sync from genesis to tip for Celestia networks (Arabica, Mocha, Mainnet)
# Usage: ./scripts/block-sync.sh --network <Arabica | Mocha | Mainnet>

set -o errexit
set -o nounset

# Default values
NODE_NAME="${NODE_NAME:-node-name}"

usage() {
  echo "Usage: $0 --network <arabica|mocha|mainnet>"
  exit 1
}

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
    ;;
  mocha)
    CHAIN_ID="mocha-4"
    SEEDS="ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@seed-mocha.pops.one:26656,258f523c96efde50d5fe0a9faeea8a3e83be22ca@seed.mocha-4.celestia.aviaone.com:20279,5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656,7da0fb48d6ef0823bc9770c0c8068dd7c89ed4ee@celest-test-seed.theamsolutions.info:443"
    ;;
  mainnet)
    CHAIN_ID="celestia"
    SEEDS="e6116822e1a5e283d8a85d3ec38f4d232274eaf3@consensus-full-seed-1.celestia-bootstrap.net:26656,cf7ac8b19ff56a9d47c75551bd4864883d1e24b5@consensus-full-seed-2.celestia-bootstrap.net:26656"
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

read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response
if [ "$response" != "y" ]; then
    echo "You must delete $CELESTIA_APP_HOME to continue."
    exit 1
fi

echo "Deleting $CELESTIA_APP_HOME..."
rm -rf "$CELESTIA_APP_HOME"

echo "Initializing config files..."
celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} > /dev/null 2>&1

echo "Setting seeds in config.toml..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" $CELESTIA_APP_HOME/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

echo "Starting celestia-appd..."
celestia-appd start
