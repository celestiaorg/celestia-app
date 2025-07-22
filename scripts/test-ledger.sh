#! /bin/bash

echo "Adding ledger key, please accept on Ledger device..."
celestia-appd keys add ledger --ledger
sleep 1

export VALIDATOR_ADDRESS=$(celestia-appd keys show validator --address)
echo "Validator address: $VALIDATOR_ADDRESS"
sleep 1

export LEDGER_ADDRESS=$(celestia-appd keys show ledger --address)
echo "Ledger address: $LEDGER_ADDRESS"
sleep 1

echo "Sending 100,000utia from validator to ledger..."
celestia-appd tx bank send $VALIDATOR_ADDRESS $LEDGER_ADDRESS 100000utia --keyring-backend test --fees 21000utia --chain-id test --yes
sleep 1

echo "Checking balance of ledger..."
celestia-appd query bank balances $LEDGER_ADDRESS
sleep 1

echo "Sending 1utia from ledger back to validator..."
celestia-appd tx bank send $LEDGER_ADDRESS $VALIDATOR_ADDRESS 1utia --keyring-backend test --fees 21000utia --chain-id test --yes
sleep 1


echo "Sending MsgTryUpgrade from ledger, please accept on Ledger device..."
celestia-appd tx signal try-upgrade --from ledger --keyring-backend test --fees 21000utia --chain-id test --yes
sleep 1

echo "Sending a MsgSignal from ledger, please accept on Ledger device..."
celestia-appd tx signal signal 4 --keyring-backend test --fees 21000utia --from ledger --chain-id test --yes
sleep 1
