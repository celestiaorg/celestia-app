#!/bin/sh

# This script tests claiming staking rewards.

set -o errexit # Stop script execution if an error is encountered
set -o nounset # Stop script execution if an undefined variable is used

celestia-appd keys add delegator

export VALIDATOR_ADDRESS=$(celestia-appd keys show validator --address)
export VALIDATOR_OPERATOR_ADDRESS=$(celestia-appd addr-conversion $VALIDATOR_ADDRESS)
export DELEGATOR_ADDRESS=$(celestia-appd keys show delegator --address)

echo "Validator address: $VALIDATOR_ADDRESS"
echo "Validator operator address: $VALIDATOR_OPERATOR_ADDRESS"
echo "Delegator address: $DELEGATOR_ADDRESS"

echo "Sending 1_000_000 utia from validator to delegator..."
celestia-appd tx bank send validator $DELEGATOR_ADDRESS 1000000utia --keyring-backend=test --fees 10000utia --yes

echo "Querying delegator balance..."
celestia-appd query bank balances $DELEGATOR_ADDRESS

echo "Delegating 100000 utia to validator..."
celestia-appd tx staking delegate $VALIDATOR_OPERATOR_ADDRESS 100000utia --keyring-backend=test --fees 10000utia --from $DELEGATOR_ADDRESS --yes

echo "Querying delegation..."
celestia-appd query staking delegation $DELEGATOR_ADDRESS $VALIDATOR_OPERATOR_ADDRESS

echo "Querying rewards..."
celestia-appd query distribution rewards $DELEGATOR_ADDRESS

echo "Unbonding from validator..."
celestia-appd tx staking unbond $VALIDATOR_OPERATOR_ADDRESS 100000utia --keyring-backend=test --fees 10000utia --from $DELEGATOR_ADDRESS --yes

echo "Querying delegation, this should return an error..."
celestia-appd query staking delegation $DELEGATOR_ADDRESS $VALIDATOR_OPERATOR_ADDRESS

echo "Withdrawing rewards..."
celestia-appd tx distribution withdraw-all-rewards --from $DELEGATOR_ADDRESS --keyring-backend=test --fees 10000utia --yes
