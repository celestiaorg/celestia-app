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
    PEERS=""
    ;;
  mocha)
    CHAIN_ID="mocha-4"
    SEEDS="b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656"
    PEERS="daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656,96b2761729cea90ee7c61206433fc0ba40c245bf@57.128.141.126:11656,f4f75a55bfc5f302ef34435ef096a4551ecb6804@152.53.33.96:12056,31bb1c9c1be7743d1115a8270bd1c83d01a9120a@148.72.141.31:26676,3e30bcfc55e7d351f18144aab4b0973e9e9bf987@65.108.226.183:11656,7a0d5818c0e5b0d4fbd86a9921f413f5e4e4ac1e@65.109.83.40:28656,43e9da043318a4ea0141259c17fcb06ecff816af@164.132.247.253:43656,5a7566aa030f7e5e7114dc9764f944b2b1324bcd@65.109.23.114:11656,c17c0cbf05e98656fee5f60fad469fc528f6d6de@65.109.25.113:11656,fb5e0b9efacc11916c58bbcd3606cbaa7d43c99f@65.108.234.84:28656,45504fb31eb97ea8778c920701fc8076e568a9cd@188.214.133.100:26656,edafdf47c443344fb940a32ab9d2067c482e59df@84.32.71.47:26656,ae7d00d6d70d9b9118c31ac0913e0808f2613a75@177.54.156.69:26656,7c841f59c35d70d9f1472d7d2a76a11eefb7f51f@136.243.69.100:43656"
    ;;
  mainnet)
    CHAIN_ID="celestia"
    SEEDS="12ad7c73c7e1f2460941326937a039139aa78884@celestia-mainnet-seed.itrocket.net:40656"
    PEERS="d535cbf8d0efd9100649aa3f53cb5cbab33ef2d6@celestia-mainnet-peer.itrocket.net:26656,4928c12dc8ba4c97bf529296c9321341c6dbcfb1@[2001:bc8:1203:1b2::8]:26656,6e4e6676108e3c54cec921d362a455172e0e3477@5.9.80.214:26101,908f49878376bd9fe78cf0ed91b51b6717418aa6@146.70.243.171:26656,d2bdd758eb6fad53f48ab4c7e612a3150c58e810@65.108.128.201:11656,0f7fea6ba71798a9daccf4c38622d8c97f747067@65.21.84.223:11656,8240e8a13594d40b6839f183795c551503309d3c@51.15.16.101:26656,aa15a9c698773412b13054b992bafc1554cfbbd7@193.35.57.185:11656,202a224f679b63cf08bf3b6f9844ff51c68fdcc9@94.237.25.73:26656,938b0387d09ae08d773cd13357d3b9b25cc540e5@136.243.94.113:26666,84970085409a8433133aedcf31f1e849bcfadd96@136.243.67.47:11656"
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

echo "Setting persistent peers in config.toml..."
sed -i -e "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"$PEERS\"/;}" $CELESTIA_APP_HOME/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

echo "Starting celestia-appd..."
celestia-appd start
