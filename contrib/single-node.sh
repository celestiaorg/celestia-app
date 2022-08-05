#!/bin/sh

set -o errexit -o nounset

CHAINID="test"

# Build genesis file incl account for passed address
coins="1000000000000000utia"
celestia-appd init $CHAINID --chain-id $CHAINID 
celestia-appd keys add validator --keyring-backend="test"
# this won't work because the some proto types are decalared twice and the logs output to stdout (dependency hell involving iavl)
celestia-appd add-genesis-account $(celestia-appd keys show validator -a --keyring-backend="test") $coins
celestia-appd gentx validator 5000000000uceles \
  --keyring-backend="test" \
  --chain-id $CHAINID \
  --orchestrator-address $(celestia-appd keys show validator -a --keyring-backend="test") \
  --ethereum-address 0x966e6f22781EF6a6A82BBB4DB3df8E225DfD9488

celestia-appd collect-gentxs

# Set proper defaults and change ports
sed -i 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_propose = "3s"/timeout_propose = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/index_all_keys = false/index_all_keys = true/g' ~/.celestia-app/config/config.toml
sed -i 's/mode = "full"/mode = "validator"/g' ~/.celestia-app/config/config.toml

# Start the celestia-app
celestia-appd start
