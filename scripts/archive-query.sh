#!/bin/sh

# This script queries the archive node for historical blocks.
# Prerequisites:
# 1. Run ./single-node-all-upgrades.sh


set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

export VALIDATOR_ADDRESS=$(celestia-appd keys show validator -a)

echo "Querying archive node for historical blocks..."
for i in {1..100}; do
    echo "Querying block $i..."
    curl -X GET "http://localhost:1317/cosmos/bank/v1beta1/balances/${VALIDATOR_ADDRESS}" -H "x-cosmos-block-height: ${i}"
    sleep 1
done
