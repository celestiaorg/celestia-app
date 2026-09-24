#!/usr/bin/env bash
set -euo pipefail
for n in 1 2; do
  rpc="http://validator-$n:26657"
  curl -fsS "$rpc/status" | jq -e '
    .result.node_info.network == "local-devnet" and
    .result.node_info.protocol_version.app == "10" and
    (.result.sync_info.latest_block_height | tonumber > 1)' >/dev/null
  curl -fsS "$rpc/validators" | jq -e '.result.validators | length == 2' >/dev/null
  curl -fsS "http://validator-$n:1317/cosmos/base/tendermint/v1beta1/node_info" >/dev/null
  nc -z "validator-$n" 9090
  nc -z "fibre-$n" "$((7979+n))"
done
node=tcp://validator-1:26657
celestia-appd query valaddr providers --node "$node" --output json |
  jq -e '[.providers[].info.host] | sort == ["fibre-1:7980", "fibre-2:7981"]' >/dev/null
address=$(jq -r .address /data/test-account.json)
celestia-appd query bank balances "$address" --node "$node" --output json |
  jq -e '.balances[] | select(.denom == "utia") | .amount | tonumber > 0' >/dev/null
celestia-appd query fibre escrow-account "$address" --node "$node" --output json |
  jq -e '.escrow_account.available_balance.amount | tonumber > 0' >/dev/null
height=$(curl -fsS http://validator-1:26657/status | jq -r .result.sync_info.latest_block_height)
sleep 3
hash1=$(curl -fsS "http://validator-1:26657/block?height=$height" | jq -er .result.block_id.hash)
hash2=$(curl -fsS "http://validator-2:26657/block?height=$height" | jq -er .result.block_id.hash)
[[ "$hash1" == "$hash2" ]]
curl -fsS http://validator-1:26657/status |
  jq -e --argjson height "$height" '.result.sync_info.latest_block_height | tonumber > $height' >/dev/null
echo 'Both v10 validators, APIs, Fibre hosts, funding, and block progress verified.'
