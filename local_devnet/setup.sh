#!/usr/bin/env bash
set -euo pipefail
node=tcp://validator-1:26657
query() { celestia-appd query "$@" --node "$node" --output json; }
submit_tx() {
  local result hash
  result=$(celestia-appd tx "$@" --node "$node" --chain-id local-devnet \
    --keyring-backend test --fees 5000utia --gas 200000 --yes --output json)
  jq -e '(.code // 0) == 0' <<<"$result" >/dev/null || { echo "$result" >&2; return 1; }
  hash=$(jq -er .txhash <<<"$result")
  for ((i=0; i<60; i++)); do
    if result=$(query tx "$hash" 2>/dev/null); then
      jq -e '.code == 0 and (.height | tonumber > 0)' <<<"$result" >/dev/null || { echo "$result" >&2; return 1; }
      echo "Committed $hash"
      return
    fi
    sleep 1
  done
  echo "Timed out waiting for $hash" >&2
  return 1
}
providers=$(curl -fsS http://validator-1:1317/valaddr/v1/all-bonded-fibre-providers)
for n in 1 2; do
  host="fibre-$n:$((7979+n))"
  if ! jq -e --arg host "$host" '.providers[] | select(.info.host == $host)' <<<"$providers" >/dev/null; then
    submit_tx valaddr set-host "$host" --from validator --home "/data/validator-$n"
  fi
done
address=$(jq -r .address /data/test-account.json)
escrow=$(query fibre escrow-account "$address")
if ! jq -e '.found' <<<"$escrow" >/dev/null; then
  submit_tx fibre deposit-to-escrow 1000000000000utia --from test --home /data/test
fi
echo "Devnet ready. Funded account: $address"
