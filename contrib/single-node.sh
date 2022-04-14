#!/bin/sh

set -o errexit -o nounset

CHAINID="test"

# Build genesis file incl account for passed address
coins="1000000000000000uceles"
celestia-appd init $CHAINID --chain-id $CHAINID 
celestia-appd keys add validator1 --keyring-backend="test"
celestia-appd keys add validator2 --keyring-backend="test"
celestia-appd keys add validator3 --keyring-backend="test"
# this won't work because the some proto types are decalared twice and the logs output to stdout (dependency hell involving iavl)
celestia-appd add-genesis-account $(celestia-appd keys show validator1 -a --keyring-backend="test") $coins
celestia-appd add-genesis-account $(celestia-appd keys show validator2 -a --keyring-backend="test") $coins
celestia-appd add-genesis-account $(celestia-appd keys show validator3 -a --keyring-backend="test") $coins
celestia-appd gentx validator1 5000000000uceles \
  --keyring-backend="test" \
  --chain-id $CHAINID \
  --orchestrator-address celes14v2rvt9az00vcd636j5q96aynzkyu0x85wuqas \
  --ethereum-address 0x91DEd26b5f38B065FC0204c7929Da6b2A21277Cd

celestia-appd collect-gentxs

# Set proper defaults and change ports
sed -i 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_propose = "3s"/timeout_propose = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/index_all_keys = false/index_all_keys = true/g' ~/.celestia-app/config/config.toml

# Start the celestia-app
celestia-appd start
