#!/bin/sh

# This script tests the Ledger integration with celestia-appd.
# 1. In a separate terminal, run: ./scripts/single-node.sh
# 2. Connect the Ledger device and open Cosmos app.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used


echo "Adding ledger key to celestia-appd keyring, please approve on Ledger..."
celestia-appd keys add ledger --ledger

export VALIDATOR_ADDRESS=$(celestia-appd --home ~/.celestia-app keys show validator -a)
export LEDGER_ADDRESS=$(celestia-appd --home ~/.celestia-app keys show ledger -a)
echo "Validator address: $VALIDATOR_ADDRESS"
echo "Ledger address: $LEDGER_ADDRESS"

echo "Sending funds from validator to ledger..."
celestia-appd tx bank send $VALIDATOR_ADDRESS $LEDGER_ADDRESS 100000utia --fees 100000utia --yes

sleep 1
echo "Querying ledger balance..."
celestia-appd query bank balances $LEDGER_ADDRESS

sleep 1

echo "Sending funds from ledger to validator..."
celestia-appd tx bank send $LEDGER_ADDRESS $VALIDATOR_ADDRESS 1utia --fees 100000utia --yes
