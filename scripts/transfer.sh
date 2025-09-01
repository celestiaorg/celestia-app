#!/bin/sh

# This script is used to test IBC between Celestia and IBC-0 (a Gaia chain managed via Gaia Manager).
# Follow the prerequisites and tutorial at https://hermes.informal.systems/tutorials/index.html
#
# Steps:
# 1. Run celestia-app via ./scripts/single-node.sh or ./scripts/single-node-upgrades.sh
# 2. Run Gaia Manager via ./bin/gm start
# 3. Set up Hermes via ./scripts/hermes.sh
# 4. Transfer tokens from Celestia to IBC-0 via ./scripts/transfer.sh


set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used


echo "--> Transferring tokens from Celestia to IBC-0"
hermes tx ft-transfer --timeout-seconds 1000 --dst-chain ibc-0 --src-chain test --src-port transfer --src-channel channel-0 --amount 100000 --denom utia


echo "--> Waiting for transfer to complete"
sleep 10


echo "--> Querying balance of IBC-0"
gaiad --node tcp://localhost:27030 query bank balances $(gaiad --home ~/.gm/ibc-0 keys --keyring-backend="test" show wallet -a)
