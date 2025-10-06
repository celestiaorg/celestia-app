#!/bin/sh

# This script starts a local single node testnet on app version 1 and then
# upgrades to app version 2, 3, 4, 5, and 6.
#
# Prerequisites:
# - Modify the `Makefile` and set V2_UPGRADE_HEIGHT = 2
# - Run `make install`

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

if ! [ -x "$(command -v celestia-appd)" ]
then
    echo "celestia-appd could not be found. Please install the celestia-appd binary using 'make install' and make sure the PATH contains the directory where the binary exists. By default, go will install the binary under '~/go/bin'"
    exit 1
fi

# Constants
CHAIN_ID="test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
FEES="200000utia"
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
    celestia-appd passthrough 1 init ${CHAIN_ID} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding a new key to the keyring..."
    celestia-appd keys add ${KEY_NAME} \
      --keyring-backend=${KEYRING_BACKEND} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Adding genesis account..."
    celestia-appd passthrough 1 add-genesis-account \
      "$(celestia-appd keys show ${KEY_NAME} -a --keyring-backend=${KEYRING_BACKEND} --home "${APP_HOME}")" \
      "1000000000000000utia" \
      --home "${APP_HOME}"

    echo "Creating a genesis tx..."
    celestia-appd passthrough 1 gentx ${KEY_NAME} 5000000000utia \
      --fees ${FEES} \
      --keyring-backend=${KEYRING_BACKEND} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Collecting genesis txs..."
    celestia-appd passthrough 1 collect-gentxs \
      --home "${APP_HOME}" \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    # Override the default RPC server listening address
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${APP_HOME}"/config/config.toml

    # Override  the log level to debug
    # sed -i'.bak' 's#log_level = "info"#log_level = "debug"#g' "${APP_HOME}"/config/config.toml

    # Override the VotingPeriod from 1 week to 1 minute
    sed -i'.bak' 's#"604800s"#"60s"#g' "${APP_HOME}"/config/genesis.json

    echo "Overriding the genesis.json app version to 1..."
    sed -i'.bak' 's/"app_version": *"[^"]*"/"app_version": "1"/' "${APP_HOME}"/config/genesis.json
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
    --delayed-precommit-timeout 1s
}

# Function to perform upgrade to a specific version
performUpgrade() {
    local target_version=$1

    echo "Submitting signal for v${target_version}..."
    celestia-appd tx signal signal ${target_version} \
        --keyring-backend=${KEYRING_BACKEND} \
        --home ${APP_HOME} \
        --from ${KEY_NAME} \
        --fees ${FEES} \
        --chain-id ${CHAIN_ID} \
        --broadcast-mode ${BROADCAST_MODE} \
        --yes

    sleep 1
    echo "Querying the tally for v${target_version}..."
    celestia-appd query signal tally ${target_version}

    echo "Submitting msg try upgrade..."
    celestia-appd tx signal try-upgrade \
        --keyring-backend=${KEYRING_BACKEND} \
        --home ${APP_HOME} \
        --from ${KEY_NAME} \
        --fees ${FEES} \
        --chain-id ${CHAIN_ID} \
        --broadcast-mode ${BROADCAST_MODE} \
        --yes

    echo "Waiting for upgrade to complete..."
    while true; do
        current_version=$(celestia-appd status | jq -r '.node_info.protocol_version.app')
        if [ "$current_version" = "${target_version}" ]; then
            echo "Upgrade to version ${target_version} complete!"
            break
        fi
        echo "Current version: $current_version, waiting for version ${target_version}..."
        sleep 1
    done
}

startUpgrades() {
    sleep 30
    echo "Waiting for app version 2 before proceeding..."
    while true; do
        current_version=$(celestia-appd status | jq -r '.node_info.protocol_version.app')
        if [ "$current_version" = "2" ]; then
            echo "App version 2 detected, proceeding with v3 upgrade..."
            break
        fi
        echo "Current version: $current_version, waiting for version 2..."
        sleep 1
    done

    # Perform upgrades to versions 3, 4, 5, and 6
    performUpgrade 3
    performUpgrade 4
    performUpgrade 5
    performUpgrade 6
}

deleteCelestiaAppHome
createGenesis
startUpgrades & # Upgrade to app version 3, 4, 5, and 6 in the background.
startCelestiaApp # Start celestia-app in the foreground.
