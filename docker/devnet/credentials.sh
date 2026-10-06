#!/usr/bin/env bash
set -euo pipefail
mkdir -p /credentials
for i in {0..10}; do
  name=node-$i
  [[ $i != 10 ]] || name=validator-0
  printf '%064x\n' "$((i + 1))" >"/credentials/$name.plaintext-key"
  celestia-appd keys import-hex "$name" "$(cat "/credentials/$name.plaintext-key")" --keyring-backend test
  echo password | celestia-appd keys export "$name" --keyring-backend test >"/credentials/$name.key"
  celestia-appd keys show "$name" -a --keyring-backend test >"/credentials/$name.addr"
done
celestia-appd keys show validator-0 --bech val -a --keyring-backend test >/credentials/validator-0.valaddr
