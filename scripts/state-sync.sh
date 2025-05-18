#!/bin/sh

# Unified state sync script for mainnet, arabica, mocha, and local (single-node) networks
# Usage:
#   ./scripts/state-sync.sh [--network <mainnet|arabica|mocha|local>] [single_node_home (for local only)]
# Defaults to mainnet if --network is not specified.

set -o errexit
set -o nounset

if ! [ -x "$(command -v celestia-appd)" ]
then
    echo "celestia-appd could not be found. Please install the celestia-appd binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

usage() {
  echo "Usage: $0 [--network <mainnet|arabica|mocha|local>] [single_node_home (for local only)]"
  exit 1
}

NETWORK="mainnet"
SINGLE_NODE_HOME="${HOME}/.celestia-app"

# Parse args
if [ $# -ge 1 ]; then
  if [ "$1" = "--network" ]; then
    if [ $# -ge 2 ]; then
      NETWORK="$2"
      if [ "$NETWORK" = "local" ] && [ $# -ge 3 ]; then
        SINGLE_NODE_HOME="$3"
      fi
    else
      usage
    fi
  elif [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    usage
  fi
fi

case "$NETWORK" in
  mainnet)
    CHAIN_ID="celestia"
    SEEDS="e6116822e1a5e283d8a85d3ec38f4d232274eaf3@consensus-full-seed-1.celestia-bootstrap.net:26656,cf7ac8b19ff56a9d47c75551bd4864883d1e24b5@consensus-full-seed-2.celestia-bootstrap.net:26656"
    RPC="https://celestia-rpc.polkachu.com:443"
    CELESTIA_APP_HOME="${HOME}/.celestia-app"
    ;;
  arabica)
    CHAIN_ID="arabica-11"
    SEEDS="827583022cc6ce65cf762115642258f937c954cd@validator-1.celestia-arabica-11.com:26656,74e42b39f512f844492ff09e30af23d54579b7bc@validator-2.celestia-arabica-11.com:26656,00d577159b2eb1f524ef9c37cb389c020a2c38d2@validator-3.celestia-arabica-11.com:26656,b2871b6dc2e18916d07264af0e87c456c2bba04f@validator-4.celestia-arabica-11.com:26656"
    RPC="https://rpc.celestia-arabica-11.com:443"
    CELESTIA_APP_HOME="${HOME}/.celestia-app"
    ;;
  mocha)
    CHAIN_ID="mocha-4"
    SEEDS="5d0bf034d6e6a8b5ee31a2f42f753f1107b3a00e@celestia-testnet-seed.itrocket.net:11656"
    RPC="https://celestia-testnet-rpc.itrocket.net:443"
    CELESTIA_APP_HOME="${HOME}/.celestia-app"

    ;;
  local)
    CHAIN_ID="test"
    KEY_NAME="validator"
    KEYRING_BACKEND="test"
    COINS="1000000000000000utia"
    DELEGATION_AMOUNT="5000000000utia"
    CELESTIA_APP_HOME="${HOME}/.celestia-app-state-sync"
    FEES="500utia"
    RPC="0.0.0.0:26657"
    GENESIS_FILE="${CELESTIA_APP_HOME}/config/genesis.json"
    ;;
  *)
    echo "Unknown network: $NETWORK"
    usage
    ;;
esac

CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)
echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""

if [ "$NETWORK" = "local" ]; then
  BLOCK_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height)
  TRUST_HASH=$(curl -s "$RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

  echo "Block height: $BLOCK_HEIGHT"
  echo "Trust hash: $TRUST_HASH"
  echo "Enabling state sync in config.toml..."
  sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
  s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"| ; \
  s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
  s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" $CELESTIA_APP_HOME/config/config.toml

  PEER=$(curl -s http://${RPC}/status | jq -r '.result.node_info.id + "@127.0.0.1:26656"')
  echo "Setting persistent peer to ${PEER}"

  createGenesis() {
    echo "Initializing validator and node config files..."
    celestia-appd init ${CHAIN_ID} \
      --chain-id ${CHAIN_ID} \
      --home "${CELESTIA_APP_HOME}" \
      > /dev/null 2>&1

    echo "Adding a new key to the keyring..."
    celestia-appd keys add ${KEY_NAME} \
      --keyring-backend=${KEYRING_BACKEND} \
      --home "${CELESTIA_APP_HOME}" \
      > /dev/null 2>&1

    echo "Copying genesis.json from the node started via ./single-node.sh..."
    cp ${SINGLE_NODE_HOME}/config/genesis.json ${CELESTIA_APP_HOME}/config/genesis.json

    # If you encounter: `sed: -I or -i may not be used with stdin` on MacOS you can mitigate by installing gnu-sed
    # https://gist.github.com/andre3k1/e3a1a7133fded5de5a9ee99c87c6fa0d?permalink_comment_id=3082272#gistcomment-3082272

    # Override the default RPC server listening address to not conflict with the node started via ./single-node.sh
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26000"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Override the p2p address to not conflict with the node started via ./single-node.sh
    sed -i'.bak' 's#laddr = "tcp://0.0.0.0:26656"#laddr = "tcp://0.0.0.0:36656"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${CELESTIA_APP_HOME}"/config/config.toml
  }

  deleteCelestiaAppHome() {
    echo "Deleting $CELESTIA_APP_HOME..."
    rm -r "$CELESTIA_APP_HOME"
  }

  startCelestiaApp() {
    echo "Starting celestia-app..."
    celestia-appd start \
        --home "${CELESTIA_APP_HOME}" \
        --grpc.enable \
        --grpc.address="0.0.0.0:9999" \
        --p2p.persistent_peers=${PEER} \
        --fast_sync false \
        --v2-upgrade-height 3
  }

  if [ -f $GENESIS_FILE ]; then
    echo "Do you want to delete existing ${CELESTIA_APP_HOME}? [y/n]"
    read -r response
    if [ "$response" = "y" ]; then
      deleteCelestiaAppHome
      createGenesis
    fi
  else
    createGenesis
  fi
  startCelestiaApp
else
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
  celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} > /dev/null 2>&1

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
  celestia-appd download-genesis ${CHAIN_ID} > /dev/null 2>&1 # Hide output to reduce terminal noise

  echo "Starting celestia-appd..."
  eval celestia-appd start --force-no-bbr
fi
