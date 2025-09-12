#!/bin/sh

# This script tests creating a vesting account with a Ledger.
# 1. In a separate terminal, run: ./scripts/single-node.sh
# 2. Connect the Ledger device and open Cosmos app.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

echo "Adding ledger key to celestia-appd keyring, please approve on Ledger..."
celestia-appd keys add ledger --ledger

echo "Adding vesting account to celestia-appd keyring..."
celestia-appd keys add vesting

export VALIDATOR_ADDRESS=$(celestia-appd --home ~/.celestia-app keys show validator -a)
export LEDGER_ADDRESS=$(celestia-appd --home ~/.celestia-app keys show ledger -a)
export VESTING_ADDRESS=$(celestia-appd --home ~/.celestia-app keys show vesting -a)

echo "Validator address: $VALIDATOR_ADDRESS"
echo "Ledger address: $LEDGER_ADDRESS"
echo "Vesting address: $VESTING_ADDRESS"

echo "Sending funds from validator to ledger..."
celestia-appd tx bank send $VALIDATOR_ADDRESS $LEDGER_ADDRESS 100000000utia --fees 100000utia --yes

sleep 1
echo "Querying ledger balance..."
celestia-appd query bank balances $LEDGER_ADDRESS

sleep 1
echo "Creating a vesting account, please approve on Ledger..."
export START_TIME=2208988800 # January 1, 2040
export END_TIME=2524608000 # January 1, 2050
celestia-appd tx vesting create-vesting-account $VESTING_ADDRESS 1utia $END_TIME --start-time $START_TIME --fees 100000utia --from $LEDGER_ADDRESS --sign-mode amino-json --yes
