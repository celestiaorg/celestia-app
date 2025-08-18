#!/bin/sh

# This script modifies the min commission rate via a governance proposal.
# Prerequisites: start single-node.sh in another terminal.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

echo "Querying staking params..."
celestia-appd query staking params

echo "Querying staking validators..."
celestia-appd query staking validators

export FROM=$(celestia-appd keys show validator --address)
echo "Submitting a governance proposal to increase the min commission rate..."
celestia-appd tx gov submit-proposal ./scripts/min-commission-rate-proposal-v1.json --from $FROM --fees 210000utia --gas 1000000 --yes

sleep 3
echo "Voting for the proposal..."
celestia-appd tx gov vote 1 yes --from $FROM --fees 210000utia --yes --gas 1000000

sleep 3
echo "Querying governance proposals..."
celestia-appd query gov proposals

echo "Waiting 30 seconds for the voting period to end..."
sleep 30

echo "Querying staking params..."
celestia-appd query staking params

echo "Querying staking validators..."
celestia-appd query staking validators
