#!/bin/sh

# This script starts a single node testnet on app version 4 that halts after 3 blocks.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

if ! [ -x "$(command -v celestia-appd)" ]
then
    echo "celestia-appd could not be found. Please install the celestia-appd binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

# Use argument as home directory if provided, else default to ~/.celestia-app
if [ $# -ge 1 ]; then
  APP_HOME="$1"
else
  APP_HOME="${HOME}/.celestia-app"
fi

# Constants
CHAIN_ID="test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
FEES="500utia"

VERSION=$(celestia-appd version 2>&1)
GENESIS_FILE="${APP_HOME}/config/genesis.json"

echo "celestia-app version: ${VERSION}"
echo "celestia-app home: ${APP_HOME}"
echo "celestia-app genesis file: ${GENESIS_FILE}"
echo ""

createGenesis() {
    echo "Initializing validator and node config files..."
    celestia-appd init ${CHAIN_ID} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding a new key to the keyring..."
    celestia-appd keys add ${KEY_NAME} \
      --keyring-backend=${KEYRING_BACKEND} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding genesis account..."
    celestia-appd genesis add-genesis-account \
      "$(celestia-appd keys show ${KEY_NAME} -a --keyring-backend=${KEYRING_BACKEND} --home "${APP_HOME}")" \
      "1000000000000000utia" \
      --home "${APP_HOME}"

    echo "Creating a genesis tx..."
    celestia-appd genesis gentx ${KEY_NAME} 5000000000utia \
      --fees ${FEES} \
      --keyring-backend=${KEYRING_BACKEND} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Collecting genesis txs..."
    celestia-appd genesis collect-gentxs \
      --home "${APP_HOME}" \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    # Override the default RPC server listening address
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${APP_HOME}"/config/config.toml

    # Override the log level to reduce noisy logs
    sed -i'.bak' 's#log_level = "info"#log_level = "*:error,p2p:info,state:info"#g' "${APP_HOME}"/config/config.toml

    # Override the halt-height to 3 blocks
    sed -i'.bak' 's#halt-height = 0#halt-height = 3#g' "${APP_HOME}"/config/app.toml
}

deleteCelestiaAppHome() {
    echo "Deleting $APP_HOME..."
    rm -r "$APP_HOME"
}

startCelestiaApp() {
  echo "Starting celestia-app..."
  celestia-appd start \
    --home "${APP_HOME}" \
    --api.enable \
    --grpc.enable \
    --grpc-web.enable \
    --timeout-commit 1s \
    --force-no-bbr # no need to require BBR usage on a local node
}

if [ -f $GENESIS_FILE ]; then
  echo "Do you want to delete existing ${APP_HOME} and start a new local testnet? [y/n]"
  read -r response
  if [ "$response" = "y" ]; then
    deleteCelestiaAppHome
    createGenesis
  else
    startCelestiaApp
  fi
else
  createGenesis
fi
startCelestiaApp
