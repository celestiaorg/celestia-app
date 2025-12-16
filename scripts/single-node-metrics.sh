#!/bin/sh

# This script starts a single node testnet with Prometheus metrics enabled.

set -o errexit
set -o nounset

if ! [ -x "$(command -v celestia-appd)" ]; then
    echo "celestia-appd could not be found. Please run 'make install'"
    exit 1
fi

# Use argument as home directory if provided, else default to ~/.celestia-app-metrics-test
APP_HOME="${1:-${HOME}/.celestia-app-metrics-test}"

# Constants
CHAIN_ID="metrics-test"
KEY_NAME="validator"
KEYRING_BACKEND="test"
FEES="500utia"

VERSION=$(celestia-appd version 2>&1)
GENESIS_FILE="${APP_HOME}/config/genesis.json"

echo "========================================="
echo "Single Node with Prometheus Metrics"
echo "========================================="
echo "celestia-app version: ${VERSION}"
echo "celestia-app home: ${APP_HOME}"
echo ""

createGenesis() {
    echo "Initializing validator and node config files..."
    celestia-appd init ${CHAIN_ID} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1

    echo "Adding a new key to the keyring..."
    celestia-appd keys add ${KEY_NAME} \
      --keyring-backend=${KEYRING_BACKEND} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1

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
      --commission-rate=0.05 \
      --commission-max-rate=1.0 \
      --commission-max-change-rate=1.0 \
      > /dev/null 2>&1

    echo "Collecting genesis txs..."
    celestia-appd genesis collect-gentxs \
      --home "${APP_HOME}" \
      > /dev/null 2>&1

    # Override the default RPC server listening address
    sed -i.bak 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i.bak 's#"null"#"kv"#g' "${APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i.bak 's#discard_abci_responses = true#discard_abci_responses = false#g' "${APP_HOME}"/config/config.toml

    # Override the log level
    sed -i.bak 's#log_level = "info"#log_level = "*:error,p2p:info,state:info,consensus:info"#g' "${APP_HOME}"/config/config.toml

    # Enable Prometheus metrics
    echo "Enabling Prometheus metrics on port 26660..."
    sed -i.bak 's#prometheus = false#prometheus = true#g' "${APP_HOME}"/config/config.toml
    sed -i.bak 's#prometheus_listen_addr = ":26660"#prometheus_listen_addr = "0.0.0.0:26660"#g' "${APP_HOME}"/config/config.toml

    # Override the VotingPeriod from 1 week to 30 seconds
    sed -i.bak 's#"604800s"#"30s"#g' "${APP_HOME}"/config/genesis.json

    echo ""
    echo "Configuration complete!"
    echo "Prometheus metrics will be available at: http://127.0.0.1:26660/metrics"
}

deleteCelestiaAppHome() {
    echo "Deleting $APP_HOME..."
    rm -rf "$APP_HOME"
}

startCelestiaApp() {
    echo ""
    echo "Starting celestia-app..."
    echo "=========================================  "
    echo "Endpoints:"
    echo "  RPC:        http://127.0.0.1:26657"
    echo "  gRPC:       127.0.0.1:9090"
    echo "  Prometheus: http://127.0.0.1:26660/metrics"
    echo "========================================="
    echo ""
    celestia-appd start \
      --home "${APP_HOME}" \
      --api.enable \
      --grpc.enable \
      --grpc-web.enable \
      --delayed-precommit-timeout 1s
}

if [ -f "$GENESIS_FILE" ]; then
    echo "Existing config found at ${APP_HOME}"
    echo "Do you want to delete and start fresh? [y/n]"
    read -r response
    if [ "$response" = "y" ]; then
        deleteCelestiaAppHome
        createGenesis
    fi
else
    createGenesis
fi

startCelestiaApp
