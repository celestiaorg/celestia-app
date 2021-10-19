#!/bin/bash


# set -e

capp=/usr/bin/celestia-appd
text="help"
coins="10000000000stake,100000000000samoleans"
test="test"
node_name="user1"
$capp $text
$capp init $test --chain-id $test
# mv /root/genesis.json /root/.celestia-app/config/genesis.json
cat ~/.celestia-app/config/genesis.json
 
$capp keys add $node_name --keyring-backend=$test
node_addr=$($capp keys show $node_name -a --keyring-backend $test)

$capp add-genesis-account $node_addr $coins

$capp gentx $node_name 5000000000stake --keyring-backend=$test --chain-id $test
$capp collect-gentxs

# Set proper defaults and change ports
sed -i 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/timeout_propose = "3s"/timeout_propose = "1s"/g' ~/.celestia-app/config/config.toml
sed -i 's/index_all_keys = false/index_all_keys = true/g' ~/.celestia-app/config/config.toml

$capp start