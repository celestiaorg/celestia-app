#!/bin/sh

# This script starts a single node that block syncs on Mocha with a halt height.

set -o errexit
set -o nounset

NODE_NAME="node-name"
CHAIN_ID="mocha-4"
SEEDS="ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@seed-mocha.pops.one:26656,258f523c96efde50d5fe0a9faeea8a3e83be22ca@seed.mocha-4.celestia.aviaone.com:20279,5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656"

CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo "chain id: ${CHAIN_ID}"
echo ""

echo "Deleting $CELESTIA_APP_HOME..."
rm -rf "$CELESTIA_APP_HOME"

echo "Initializing config files..."
celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} > /dev/null 2>&1

echo "Setting seeds in config.toml..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" $CELESTIA_APP_HOME/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

echo "Setting halt height to 5..."
sed -i.bak -e "s/^halt-height *=.*/halt-height = 5/" $CELESTIA_APP_HOME/config/app.toml

echo "Starting celestia-appd..."
celestia-appd start
