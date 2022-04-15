#!/bin/sh

set -o errexit -o nounset

CHAINID="test"

# Build genesis file incl account for passed address
coins="1000000000000000uceles"
celestia-appd init $CHAINID --chain-id $CHAINID 
#celestia-appd keys add validator1 --keyring-backend="test"
celestia-appd keys add validator2 --keyring-backend="test"
celestia-appd keys add validator3 --keyring-backend="test"
# this won't work because the some proto types are decalared twice and the logs output to stdout (dependency hell involving iavl)
celestia-appd add-genesis-account $(celestia-appd keys show validator1 -a --keyring-backend="test") $coins
celestia-appd add-genesis-account $(celestia-appd keys show validator2 -a --keyring-backend="test") $coins
celestia-appd add-genesis-account $(celestia-appd keys show validator3 -a --keyring-backend="test") $coins
celestia-appd gentx validator1 5000000000uceles \
  --keyring-backend="test" \
  --chain-id $CHAINID \
  --orchestrator-address $(celestia-appd keys show validator1 -a --keyring-backend="test") \
  --ethereum-address 0x966e6f22781EF6a6A82BBB4DB3df8E225DfD9488

celestia-appd collect-gentxs

# Set proper defaults and change ports
sed -i 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_propose = "3s"/timeout_propose = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/index_all_keys = false/index_all_keys = true/g' ~/.celestia-app/config/config.toml

# Start the celestia-app
celestia-appd start

#celestia-appd tx staking create-validator \
# --amount=1000001celes \
# --pubkey=$(celestia-appd tendermint show-validator --home ~/.celestia-app/another_home4) \
# --moniker=$MONIKER \
# --chain-id=devnet-2 \
# --commission-rate=0.1 \
# --commission-max-rate=0.2 \
# --commission-max-change-rate=0.01 \
# --min-self-delegation=1000000 \
# --from=validator2 \
# --keyring-backend=test \
# --orchestrator-address celes17zxe3qwtzec7dzggcm27f597x3j66kt9wf2tht \
# --ethereum-address 0x966e6f22781EF6a6A82BBB4DB3df8E225DfD9488