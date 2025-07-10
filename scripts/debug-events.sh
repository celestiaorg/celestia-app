echo "Getting validator address..."
VALIDATOR_ADDRESS=$(celestia-appd keys show validator --address)
echo "Got validator address: $VALIDATOR_ADDRESS"

echo "Sending a bank send transaction..."
celestia-appd tx bank send $VALIDATOR_ADDRESS $VALIDATOR_ADDRESS 1utia --fees 20000utia --yes
echo "Sent bank send transaction"

echo "Querying for events..."
celestia-appd query txs --query "message.sender='$VALIDATOR_ADDRESS'"
echo "Queried for events"
