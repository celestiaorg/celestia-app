#!/bin/sh

# This script will upgrade the node from v2 -> v3.
# Prerequisites: ensure ./single-node.sh is running in another terminal.
# Wait until block height is 3 for the node to upgrade from v1 -> v2.

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
CELESTIA_APP_HOME="${HOME}/.celestia-app"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)
FEES="500utia"
BROADCAST_MODE="block"

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "celestia-app version: ${CELESTIA_APP_VERSION}"
echo ""


echo "Submitting signal for v3..."
celestia-appd tx signal signal 3 \
    --keyring-backend=${KEYRING_BACKEND} \
    --home ${CELESTIA_APP_HOME} \
    --from ${KEY_NAME} \
    --fees ${FEES} \
    --chain-id ${CHAIN_ID} \
    --broadcast-mode ${BROADCAST_MODE} \
    --yes

echo "Querying the tally for v3..."
celestia-appd query signal tally 3

echo "Submitting msg try upgrade..."
celestia-appd tx signal try-upgrade \
    --keyring-backend=${KEYRING_BACKEND} \
    --home ${CELESTIA_APP_HOME} \
    --from ${KEY_NAME} \
    --fees ${FEES} \
    --chain-id ${CHAIN_ID} \
    --broadcast-mode ${BROADCAST_MODE} \
    --yes

echo "Querying for pending upgrade..."
celestia-appd query signal upgrade
