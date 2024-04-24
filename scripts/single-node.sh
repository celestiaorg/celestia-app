#!/bin/sh

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

CHAIN_ID="private"
KEY_NAME="validator"
KEYRING_BACKEND="test"
COINS="1000000000000000utia"
DELEGATION_AMOUNT="5000000000utia"
CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)
FEES="500utia"

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""

# Ask the user for confirmation before deleting the existing celestia-app home
# directory.
read -p "Are you sure you want to delete: $CELESTIA_APP_HOME? [y/n] " response

# Check the user's response
if [[ $response != "y" ]]; then
    # Exit if the user did not respond with "y"
    echo "You must delete $CELESTIA_APP_HOME to continue."
    exit 1
fi

echo "Deleting $CELESTIA_APP_HOME..."
rm -r "$CELESTIA_APP_HOME"

echo "Initializing validator and node config files..."
celestia-appd init ${CHAIN_ID} \
  --chain-id ${CHAIN_ID} \
  --home ${CELESTIA_APP_HOME} \
  &> /dev/null # Hide output to reduce terminal noise

echo "Adding a new key to the keyring..."
celestia-appd keys add ${KEY_NAME} \
  --keyring-backend=${KEYRING_BACKEND} \
  --home ${CELESTIA_APP_HOME} \
  &> /dev/null # Hide output to reduce terminal noise

echo "Adding genesis account..."
celestia-appd add-genesis-account \
  $(celestia-appd keys show ${KEY_NAME} -a --keyring-backend=${KEYRING_BACKEND} --home ${CELESTIA_APP_HOME}) \
  $COINS \
  --home ${CELESTIA_APP_HOME}

echo "Creating a genesis tx..."
celestia-appd gentx ${KEY_NAME} ${DELEGATION_AMOUNT} \
  --fees ${FEES} \
  --keyring-backend=${KEYRING_BACKEND} \
  --chain-id ${CHAIN_ID} \
  --home ${CELESTIA_APP_HOME} \
  &> /dev/null # Hide output to reduce terminal noise

echo "Collecting genesis txs..."
celestia-appd collect-gentxs \
  --home ${CELESTIA_APP_HOME} \
    &> /dev/null # Hide output to reduce terminal noise

# Set proper defaults and change ports
# If you encounter: `sed: -I or -i may not be used with stdin` on MacOS you can mitigate by installing gnu-sed
# https://gist.github.com/andre3k1/e3a1a7133fded5de5a9ee99c87c6fa0d?permalink_comment_id=3082272#gistcomment-3082272
sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' "${CELESTIA_APP_HOME}"/config/config.toml

# Enable transaction indexing
sed -i'.bak' 's#"null"#"kv"#g' "${CELESTIA_APP_HOME}"/config/config.toml

# Persist ABCI responses
sed -i'.bak' 's#discard_abci_responses = true#discard_abci_responses = false#g' "${CELESTIA_APP_HOME}"/config/config.toml

# Override the VotingPeriod from 1 week to 1 minute
sed -i'.bak' 's#"604800s"#"60s"#g' "${CELESTIA_APP_HOME}"/config/genesis.json

# Override the genesis to use app version 1 and then upgrade to app version 2 later.
sed -i'.bak' 's#"app_version": "2"#"app_version": "1"#g' "${CELESTIA_APP_HOME}"/config/genesis.json

# Start celestia-app
echo "Starting celestia-app..."
celestia-appd start \
  --home ${CELESTIA_APP_HOME} \
  --api.enable \
  --grpc.enable \
  --grpc-web.enable \
  --v2-upgrade-height 3

# Example QGB tx
# celestia-appd tx qgb register \
#   "$(celestia-appd keys show validator --home ${CELESTIA_APP_HOME} --bech val -a)" \
#   0x966e6f22781EF6a6A82BBB4DB3df8E225DfD9488 \
#   --from ${KEY_NAME} \
#   --home ${CELESTIA_APP_HOME} \
#   --fees 30000utia \
#   --broadcast-mode block \
#   --yes
