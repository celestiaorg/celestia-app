#!/bin/sh

# This script starts a local single node testnet on app version 4 and then upgrades to app version 5.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

CHAIN_ID="test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
FEES="50000utia"
BROADCAST_MODE="sync"
APP_HOME="${HOME}/.celestia-app"
GENESIS_FILE="${APP_HOME}/config/genesis.json"
VERSION=$(celestia-appd version 2>&1)

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

    # Override the log level to debug
    # sed -i'.bak' 's#log_level = "info"#log_level = "debug"#g' "${APP_HOME}"/config/config.toml
    sed -i'.bak' 's#log_level = "info"#log_level = "*:error,p2p:info,state:info"#g' "${APP_HOME}"/config/config.toml
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
    --force-no-bbr
}

upgradeToV5() {
    sleep 30
    echo "Submitting signal for v5..."
    celestia-appd tx signal signal 5 \
        --keyring-backend=${KEYRING_BACKEND} \
        --home ${APP_HOME} \
        --from ${KEY_NAME} \
        --fees ${FEES} \
        --chain-id ${CHAIN_ID} \
        --broadcast-mode ${BROADCAST_MODE} \
        --yes
        # > /dev/null 2>&1 # Hide output to reduce terminal noise

    sleep 10
    echo "Querying the tally for v5..."
    celestia-appd query signal tally 5

    sleep 10
    echo "Submitting msg try upgrade..."
    celestia-appd tx signal try-upgrade \
        --keyring-backend=${KEYRING_BACKEND} \
        --home ${APP_HOME} \
        --from ${KEY_NAME} \
        --fees ${FEES} \
        --chain-id ${CHAIN_ID} \
        --broadcast-mode ${BROADCAST_MODE} \
        --yes
        # > /dev/null 2>&1 # Hide output to reduce terminal noise

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

upgradeToV5 & # Start the upgrade process from v4 -> v5 in the background.
startCelestiaApp # Start celestia-app in the foreground.
