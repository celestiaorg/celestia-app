#!/bin/sh

# This script starts a single node and attempts to state sync with a node
# started via ./single-node.sh

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

if ! [ -x "$(command -v celestia-appd)" ]
then
    echo "celestia-appd could not be found. Please install the celestia-appd binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

CHAIN_ID="test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
COINS="1000000000000000utia"
DELEGATION_AMOUNT="5000000000utia"
SINGLE_NODE_HOME="${HOME}/.celestia-app"
CELESTIA_APP_HOME="${HOME}/.celestia-app-state-sync"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)
GENESIS_FILE="${CELESTIA_APP_HOME}/config/genesis.json"
FEES="500utia"
RPC="0.0.0.0:26657"

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""

BLOCK_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height);
TRUST_HASH=$(curl -s "$RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)

echo "Block height: $BLOCK_HEIGHT"
echo "Trust hash: $TRUST_HASH"
echo "Enabling state sync in config.toml..."
sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" $HOME/.celestia-app/config/config.toml

PEER=$(curl -s http://${RPC}/status | jq -r '.result.node_info.id + "@127.0.0.1:26656"')
echo "Setting persistent peer to ${PEER}"

createGenesis() {
    echo "Initializing validator and node config files..."
    celestia-appd init ${CHAIN_ID} \
      --chain-id ${CHAIN_ID} \
      --home "${CELESTIA_APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding a new key to the keyring..."
    celestia-appd keys add ${KEY_NAME} \
      --keyring-backend=${KEYRING_BACKEND} \
      --home "${CELESTIA_APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Copying genesis.json from the node started via ./single-node.sh..."
    cp ${SINGLE_NODE_HOME}/config/genesis.json ${CELESTIA_APP_HOME}/config/genesis.json

    # If you encounter: `sed: -I or -i may not be used with stdin` on MacOS you can mitigate by installing gnu-sed
    # https://gist.github.com/andre3k1/e3a1a7133fded5de5a9ee99c87c6fa0d?permalink_comment_id=3082272#gistcomment-3082272

    # Override the default RPC server listening address to not conflict with the node started via ./single-node.sh
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26000"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Override the p2p address to not conflict with the node started via ./single-node.sh
    sed -i'.bak' 's#laddr = "tcp://0.0.0.0:26656"#laddr = "tcp://0.0.0.0:36656"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Set a persistent peer that is the node started via ./single-node.sh
    # sed -i'.bak' "s#persistent_peers = \"\"#persistent_peers = \"${PEER}\"#g" "${CELESTIA_APP_HOME}/config/config.toml"

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Override the log level to debug
    # sed -i'.bak' 's#log_level = "info"#log_level = "debug"#g' "${CELESTIA_APP_HOME}"/config/config.toml
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
