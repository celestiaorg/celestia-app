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

echo "Creating ~/Downloads/wallet.json"
rm -f ~/Downloads/wallet.json
touch ~/Downloads/wallet.json

echo "--> Adding wallet"
celestia-appd keys add wallet --output json > ~/Downloads/wallet.json

export VALIDATOR=$(celestia-appd keys show validator --address)
echo "--> Validator address: $VALIDATOR"

export WALLET=$(celestia-appd keys show wallet --address)
echo "--> Wallet address: $WALLET"

# Fund the wallet address
echo "--> Funding wallet"
celestia-appd tx bank send $VALIDATOR $WALLET 10000000utia --fees 1000000utia --chain-id test --yes

echo "--> Importing wallet into Hermes"
hermes keys add --chain test --key-file ~/Downloads/wallet.json --overwrite

echo "--> Creating new connection"
hermes create channel --a-chain ibc-0 --b-chain test --a-port transfer --b-port transfer --new-client-connection

echo "--> Starting relayer"
hermes start
