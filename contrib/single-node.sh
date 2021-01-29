#!/bin/sh

set -o errexit -o nounset

CHAINID=$1
GENACCT=$2

if [ -z "$1" ]; then
  echo "Need to input chain id..."
  exit 1
fi

if [ -z "$2" ]; then
  echo "Need to input genesis account address..."
  exit 1
fi

# Build genesis file incl account for passed address
coins="10000000000stake,100000000000samoleans"
lazyledger-appd init $CHAINID --chain-id $CHAINID 
lazyledger-appd keys add validator --keyring-backend="test"
# this won't work because the some proto types are decalared twice and the logs output to stdout (dependency hell involving iavl)
lazyledger-appd add-genesis-account $(lazyledger-appd keys show validator -a --keyring-backend="test") $coins
lazyledger-appd add-genesis-account $GENACCT $coins
lazyledger-appd gentx validator 5000000000stake --keyring-backend="test" --chain-id $CHAINID
lazyledger-appd collect-gentxs

# Set proper defaults and change ports
sed -i 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' ~/.lazyledger-app/config/config.toml
sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/g' ~/.lazyledger-app/config/config.toml
sed -i 's/timeout_propose = "3s"/timeout_propose = "1s"/g' ~/.lazyledger-app/config/config.toml
sed -i 's/index_all_keys = false/index_all_keys = true/g' ~/.lazyledger-app/config/config.toml

# Start the lazyledger-app
lazyledger-appd start
