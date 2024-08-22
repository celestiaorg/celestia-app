#!/bin/sh

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
CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)
GENESIS_FILE="${CELESTIA_APP_HOME}/config/genesis.json"
FEES="500utia"

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""

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

    echo "Adding genesis account..."
    celestia-appd add-genesis-account \
      "$(celestia-appd keys show ${KEY_NAME} -a --keyring-backend=${KEYRING_BACKEND} --home "${CELESTIA_APP_HOME}")" \
      $COINS \
      --home "${CELESTIA_APP_HOME}"

    echo "Creating a genesis tx..."
    celestia-appd gentx ${KEY_NAME} ${DELEGATION_AMOUNT} \
      --fees ${FEES} \
      --keyring-backend=${KEYRING_BACKEND} \
      --chain-id ${CHAIN_ID} \
      --home "${CELESTIA_APP_HOME}" \
      > /dev/null 2>&1 # Hide output to reduce terminal noise

    echo "Collecting genesis txs..."
    celestia-appd collect-gentxs \
      --home "${CELESTIA_APP_HOME}" \
        > /dev/null 2>&1 # Hide output to reduce terminal noise

    # If you encounter: `sed: -I or -i may not be used with stdin` on MacOS you can mitigate by installing gnu-sed
    # https://gist.github.com/andre3k1/e3a1a7133fded5de5a9ee99c87c6fa0d?permalink_comment_id=3082272#gistcomment-3082272

    # Override the default RPC servier listening address
    sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Enable transaction indexing
    sed -i'.bak' 's#"null"#"kv"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Persist ABCI responses
    sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Override the log level to debug
    # sed -i'.bak' 's#log_level = "info"#log_level = "debug"#g' "${CELESTIA_APP_HOME}"/config/config.toml

    # Override the VotingPeriod from 1 week to 1 minute
    sed -i'.bak' 's#"604800s"#"60s"#g' "${CELESTIA_APP_HOME}"/config/genesis.json

    # Override the genesis to use app version 1 and then upgrade to app version 2 later.
    sed -i'.bak' 's#"app_version": "2"#"app_version": "1"#g' "${CELESTIA_APP_HOME}"/config/genesis.json

    trace_type="local"
    sed -i.bak -e "s/^trace_type *=.*/trace_type = \"$trace_type\"/" ${CELESTIA_APP_HOME}/config/config.toml

    trace_pull_address=":26661"
    sed -i.bak -e "s/^trace_pull_address *=.*/trace_pull_address = \"$trace_pull_address\"/" ${CELESTIA_APP_HOME}/config/config.toml

    trace_push_batch_size=1000
    sed -i.bak -e "s/^trace_push_batch_size *=.*/trace_push_batch_size = \"$trace_push_batch_size\"/" ${CELESTIA_APP_HOME}/config/config.toml

    echo "Tracing is set up with the ability to pull traced data from the node on the address http://127.0.0.1${trace_pull_address}"
}

deleteCelestiaAppHome() {
    echo "Deleting $CELESTIA_APP_HOME..."
    rm -r "$CELESTIA_APP_HOME"
}

startCelestiaApp() {
  echo "Starting celestia-app..."
  celestia-appd start \
    --home "${CELESTIA_APP_HOME}" \
    --api.enable \
    --grpc.enable \
    --grpc-web.enable \
    --v2-upgrade-height 3 \
}

if [ -f $GENESIS_FILE ]; then
  echo "Do you want to delete existing ${CELESTIA_APP_HOME} and start a new local testnet? [y/n]"
  read -r response
  if [ "$response" = "y" ]; then
    deleteCelestiaAppHome
    createGenesis
  fi
else
  createGenesis
fi
startCelestiaApp
