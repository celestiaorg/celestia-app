#!/bin/sh

# Start a consensus node with state sync for Celestia networks (Arabica, Mocha, Mainnet)
# Usage: ./scripts/state-sync.sh --network <arabica|mocha|mainnet>

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
    RPC="https://rpc.celestia-arabica-11.com:443"
    SEEDS="827583022cc6ce65cf762115642258f937c954cd@validator-1.celestia-arabica-11.com:26656,74e42b39f512f844492ff09e30af23d54579b7bc@validator-2.celestia-arabica-11.com:26656,00d577159b2eb1f524ef9c37cb389c020a2c38d2@validator-3.celestia-arabica-11.com:26656,b2871b6dc2e18916d07264af0e87c456c2bba04f@validator-4.celestia-arabica-11.com:26656"
    ;;
  mocha)
    CHAIN_ID="mocha-4"
    RPC="https://celestia-testnet-rpc.itrocket.net:443"
    SEEDS="ee9f90974f85c59d3861fc7f7edb10894f6ac3c8@seed-mocha.pops.one:26656,258f523c96efde50d5fe0a9faeea8a3e83be22ca@seed.mocha-4.celestia.aviaone.com:20279,5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656,7da0fb48d6ef0823bc9770c0c8068dd7c89ed4ee@celest-test-seed.theamsolutions.info:443"
    ;;
  mainnet)
    CHAIN_ID="celestia"
    RPC="https://celestia-rpc.polkachu.com:443"
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

read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response
if [ "$response" != "y" ]; then
    echo "You must delete $CELESTIA_APP_HOME to continue."
    exit 1
fi

echo "Deleting $CELESTIA_APP_HOME..."
rm -rf "$CELESTIA_APP_HOME"

echo "Initializing config files..."
celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} --home "${CELESTIA_APP_HOME}" > /dev/null 2>&1

echo "Setting seeds in config.toml..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" $CELESTIA_APP_HOME/config/config.toml

echo "Downloading genesis file..."
celestia-appd download-genesis ${CHAIN_ID} --home "${CELESTIA_APP_HOME}" > /dev/null 2>&1

echo "Getting state sync parameters..."
BLOCK_HEIGHT=$(($(curl -s ${RPC}/block | jq -r .result.block.header.height) - 10000)) # subtract a variable amount of blocks to ensure we can sync
TRUST_HASH=$(curl -s ${RPC}/block?height=${BLOCK_HEIGHT} | jq -r .result.block_id.hash)
echo "RPC: ${RPC}"
echo "Block height: ${BLOCK_HEIGHT}"
echo "Trust hash: ${TRUST_HASH}"
echo "Enabling state sync in config.toml..."
sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"${RPC},${RPC}\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1${BLOCK_HEIGHT}| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"${TRUST_HASH}\"|" $CELESTIA_APP_HOME/config/config.toml

# Override the p2p address
sed -i'.bak' 's#laddr = "tcp://0.0.0.0:26656"#laddr = "tcp://0.0.0.0:36656"#g' "${CELESTIA_APP_HOME}"/config/config.toml

# Enable transaction indexing
sed -i'.bak' 's#"null"#"kv"#g' "${CELESTIA_APP_HOME}"/config/config.toml

# Persist ABCI responses
sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${CELESTIA_APP_HOME}"/config/config.toml

# List of tracing tables
sed -i'.bak' 's#tracing_tables = ".*"#tracing_tables = "peers,pending_bytes,received_bytes,abci"#g' "${CELESTIA_APP_HOME}"/config/config.toml

# Trace type
sed -i'.bak' 's#trace_type = ".*"#trace_type = "local"#g' "${CELESTIA_APP_HOME}"/config/config.toml

echo "Starting celestia-appd..."
celestia-appd start --force-no-bbr
