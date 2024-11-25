#!/bin/sh

# This script starts a single node testnet on app version 3.

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

VERSION=$(celestia-appd version 2>&1)
APP_HOME="${HOME}/.celestia-app"
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
    celestia-appd add-genesis-account \
      "$(celestia-appd keys show ${KEY_NAME} -a --keyring-backend=${KEYRING_BACKEND} --home "${APP_HOME}")" \
      "1000000000000000utia" \
      --home "${APP_HOME}"

    echo "Creating a genesis tx..."
    celestia-appd gentx ${KEY_NAME} 5000000000utia \
      --fees ${FEES} \
      --keyring-backend=${KEYRING_BACKEND} \
      --chain-id ${CHAIN_ID} \
      --home "${APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Collecting genesis txs..."
    celestia-appd collect-gentxs \
      --home "${APP_HOME}" \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    # If you encounter: `sed: -I or -i may not be used with stdin` on MacOS you can mitigate by installing gnu-sed
    # https://gist.github.com/andre3k1/e3a1a7133fded5de5a9ee99c87c6fa0d?permalink_comment_id=3082272#gistcomment-3082272

    # Override the default RPC server listening address
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${APP_HOME}"/config/config.toml

    # Override the log level to debug
    # sed -i'.bak' 's#log_level = "info"#log_level = "debug"#g' "${APP_HOME}"/config/config.toml

    # Override the VotingPeriod from 1 week to 1 minute
    sed -i'.bak' 's#"604800s"#"60s"#g' "${APP_HOME}"/config/genesis.json

    trace_type="local"
    sed -i.bak -e "s/^trace_type *=.*/trace_type = \"$trace_type\"/" ${APP_HOME}/config/config.toml

    trace_pull_address=":26661"
    sed -i.bak -e "s/^trace_pull_address *=.*/trace_pull_address = \"$trace_pull_address\"/" ${APP_HOME}/config/config.toml

    trace_push_batch_size=1000
    sed -i.bak -e "s/^trace_push_batch_size *=.*/trace_push_batch_size = \"$trace_push_batch_size\"/" ${APP_HOME}/config/config.toml

    echo "Tracing is set up with the ability to pull traced data from the node on the address http://127.0.0.1${trace_pull_address}"
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
    --force-no-bbr # no need to require BBR usage on a local node
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
startCelestiaApp
