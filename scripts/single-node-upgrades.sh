#!/bin/sh

# This script starts a local single node testnet on app version 3 and then upgrades to app version 4.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

if ! [ -x "$(command -v celestia-appd)" ]
then
    echo "celestia-appd could not be found. Please install the celestia-appd binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

# Constants
CHAIN_ID="test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
FEES="500utia"
BROADCAST_MODE="sync"

# Use argument as home directory if provided, else default to ~/.celestia-app
if [ $# -ge 1 ]; then
  APP_HOME="$1"
else
  APP_HOME="${HOME}/.celestia-app"
fi

VERSION=$(celestia-appd version 2>&1)
GENESIS_FILE="${APP_HOME}/config/genesis.json"

echo "celestia-app version: ${VERSION}"
echo "celestia-app home: ${APP_HOME}"
echo "celestia-app genesis file: ${GENESIS_FILE}"
echo ""

createGenesis() {
    echo "Initializing validator and node config files..."
    celestia-appd passthrough 3 init ${CHAIN_ID} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding a new key to the keyring..."
    celestia-appd keys add ${KEY_NAME} \
      --keyring-backend=${KEYRING_BACKEND} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding genesis account..."
    celestia-appd passthrough 3 add-genesis-account \
      "$(celestia-appd keys show ${KEY_NAME} -a --keyring-backend=${KEYRING_BACKEND} --home "${APP_HOME}")" \
      "1000000000000000utia" \
      --home "${APP_HOME}"

    echo "Creating a genesis tx..."
    celestia-appd passthrough 3 gentx ${KEY_NAME} 5000000000utia \
      --fees ${FEES} \
      --keyring-backend=${KEYRING_BACKEND} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Collecting genesis txs..."
    celestia-appd passthrough 3 collect-gentxs \
      --home "${APP_HOME}" \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    # Override the default RPC server listening address
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${APP_HOME}"/config/config.toml

    # Override the VotingPeriod from 1 week to 1 minute
    sed -i'.bak' 's#"604800s"#"60s"#g' "${APP_HOME}"/config/genesis.json

    # Override the log level to debug
    # sed -i'.bak' 's#log_level = "info"#log_level = "debug"#g' "${APP_HOME}"/config/config.toml

    # HACKHACK: the multiplexer can not read the app_version field, so we need to use app instead.
    # Override the genesis to use app version 3.
    sed -i'.bak' 's/"app_version": *"[^"]*"/"app": "3"/' "${APP_HOME}"/config/genesis.json
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
    --force-no-bbr \
    --address tcp://127.0.0.1:26658 \
    --proxy_app tcp://127.0.0.1:26658
}

upgradeToV4() {
    sleep 30
    echo "Submitting signal for v4..."
    celestia-appd tx signal signal 4 \
        --keyring-backend=${KEYRING_BACKEND} \
        --home ${APP_HOME} \
        --from ${KEY_NAME} \
        --fees ${FEES} \
        --chain-id ${CHAIN_ID} \
        --broadcast-mode ${BROADCAST_MODE} \
        --yes \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    sleep 10
    echo "Querying the tally for v4..."
    celestia-appd query signal tally 4

    echo "Submitting msg try upgrade..."
    celestia-appd tx signal try-upgrade \
        --keyring-backend=${KEYRING_BACKEND} \
        --home ${APP_HOME} \
        --from ${KEY_NAME} \
        --fees ${FEES} \
        --chain-id ${CHAIN_ID} \
        --broadcast-mode ${BROADCAST_MODE} \
        --yes \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    sleep 10
    echo "Querying for pending upgrade..."
    celestia-appd query signal upgrade
}

if [ -f $GENESIS_FILE ]; then
  echo "Do you want to delete existing ${APP_HOME} and start a new local testnet? [y/n]"
  read -r response
  if [ "$response" = "y" ]; then
    deleteCelestiaAppHome
    createGenesis
  fi
else
  createGenesis
fi

upgradeToV4 & # Start the upgrade process from v3 -> v4 in the background.
startCelestiaApp # Start celestia-app in the foreground.
