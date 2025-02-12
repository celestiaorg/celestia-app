#!/usr/bin/env bash

CELESTIA_BIN=${CELESTIA_BIN:=$(which celestia-appd 2>/dev/null)}
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
PWD=$(pwd)

if [ -z "$CELESTIA_BIN" ]; then echo "CELESTIA_BIN is not set. Make sure to run 'make install' before"; exit 1; fi
CELESTIA_HOME=$($CELESTIA_BIN config home)
if [ -d "$CELESTIA_HOME" ]; then rm -r $CELESTIA_HOME; fi
$CELESTIA_BIN config set client chain-id local_devnet
$CELESTIA_BIN config set client keyring-backend test
$CELESTIA_BIN config set client keyring-default-keyname alice
$CELESTIA_BIN config set app api.enable true
$CELESTIA_BIN keys add alice
cd $SCRIPT_DIR; cp ../celestia-app/genesis.json $CELESTIA_HOME/config/genesis.json; cd $PWD # use local_devnet genesis
$CELESTIA_BIN genesis add-genesis-account alice 5000000000utia --keyring-backend test
$CELESTIA_BIN genesis gentx alice 1000000utia --chain-id local_devnet
$CELESTIA_BIN genesis collect-gentxs
