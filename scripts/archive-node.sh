#!/bin/sh

# Start an archive node and block sync from genesis on Mainnet.

set -o errexit
set -o nounset

NODE_NAME="archive-node"
SEEDS="12ad7c73c7e1f2460941326937a039139aa78884@celestia-mainnet-seed.itrocket.net:40656"
PEERS="d535cbf8d0efd9100649aa3f53cb5cbab33ef2d6@celestia-mainnet-peer.itrocket.net:26656,4928c12dc8ba4c97bf529296c9321341c6dbcfb1@[2001:bc8:1203:1b2::8]:26656,6e4e6676108e3c54cec921d362a455172e0e3477@5.9.80.214:26101,908f49878376bd9fe78cf0ed91b51b6717418aa6@146.70.243.171:26656,d2bdd758eb6fad53f48ab4c7e612a3150c58e810@65.108.128.201:11656,0f7fea6ba71798a9daccf4c38622d8c97f747067@65.21.84.223:11656,8240e8a13594d40b6839f183795c551503309d3c@51.15.16.101:26656,aa15a9c698773412b13054b992bafc1554cfbbd7@193.35.57.185:11656,202a224f679b63cf08bf3b6f9844ff51c68fdcc9@94.237.25.73:26656,938b0387d09ae08d773cd13357d3b9b25cc540e5@136.243.94.113:26666,84970085409a8433133aedcf31f1e849bcfadd96@136.243.67.47:11656"

CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)
CHAIN_ID="celestia"

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

echo "Setting persistent peers in config.toml..."
sed -i -e "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"$PEERS\"/;}" $CELESTIA_APP_HOME/config/config.toml

echo "Setting pruning to nothing..."
sed -i.bak -e "s/^pruning *=.*/pruning = \"nothing\"/" $CELESTIA_APP_HOME/config/app.toml

echo "Setting discard_abci_responses to false..."
sed -i.bak -e "s/^discard_abci_responses *=.*/discard_abci_responses = false/" $CELESTIA_APP_HOME/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

echo "Starting celestia-appd..."
celestia-appd start
