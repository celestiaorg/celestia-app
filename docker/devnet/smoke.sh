#!/usr/bin/env bash
set -euo pipefail
dir=$(dirname "$(realpath "$0")")
export DEVNET_VERSION=${DEVNET_VERSION:-local}
project=celestia-devnet-test-$$
log_dir=${DEVNET_LOG_DIR:-/tmp/$project}
mkdir -p "$log_dir"
dc() { docker compose -p "$project" -f "$dir/compose.yaml" -f "$dir/compose.test.yaml" "$@"; }
cleanup() {
  dc logs --no-color >"$log_dir/final.log" 2>&1
  dc down -v --remove-orphans
}
trap cleanup EXIT
trap 'echo "Smoke test failed at line $LINENO; logs: $log_dir" >&2' ERR
height() { dc exec -T validator curl -fsS http://127.0.0.1:26657/status | jq -er '.result.sync_info.latest_block_height | tonumber'; }
block1_hash() { dc exec -T validator curl -fsS 'http://127.0.0.1:26657/block?height=1' | jq -er .result.block_id.hash; }
pff() { dc exec -T validator submit --grpc 127.0.0.1:9090 --keyring-dir /data/validator --key node-0 --validators 1 --kind pff "$@"; }
start=$SECONDS
dc up -d --wait --wait-timeout 240
echo "Cached-image startup: $((SECONDS - start))s"
curl -fsS "http://$(dc port validator 26657)/status" | jq -e '.result.node_info.network == "devnet"' >/dev/null
initial_hash=$(block1_hash)
dc exec -T validator bash -seuo pipefail <<'EOF'
  for name in validator-0 node-{0..9}; do
    address=$(cat "/credentials/$name.addr")
    jq -e --arg address "$address" '.app_state.bank.balances[] | select(.address == $address) | .coins == [{denom: "utia", amount: "1000000000000000"}]' /data/validator/config/genesis.json >/dev/null
    celestia-appd query fibre escrow-account "$address" --home /data/validator --node tcp://127.0.0.1:26657 --output json |
      jq -e '.found and .escrow_account.balance.amount == "1000000000000" and .escrow_account.available_balance.amount == "1000000000000"' >/dev/null
  done
  for i in {0..9}; do
    celestia-appd query bank balances "$(cat "/credentials/node-$i.addr")" --home /data/validator --node tcp://127.0.0.1:26657 --output json |
      jq -e '.balances == [{denom: "utia", amount: "1000000000000000"}]' >/dev/null
  done
EOF
dc exec -T validator submit --grpc 127.0.0.1:9090 --keyring-dir /data/validator --key node-0 --kind pfb
pff --size 262144 --verify --save /data/receipt.json
initial_height=$(height)
dc stop
[[ $(docker inspect -f '{{.State.ExitCode}}' "$(dc ps -aq validator)") == 0 ]]
dc up -d --wait --wait-timeout 240
[[ $(block1_hash) == "$initial_hash" && $(height) -gt "$initial_height" ]]
pff --read /data/receipt.json
for process in fibre celestia-appd; do
  dc exec -T validator pkill -KILL -x "$process"
  code=$(docker wait "$(dc ps -aq validator)")
  ((code != 0))
  dc logs --no-color >"$log_dir/$process-failure.log"
  dc down
  dc up -d --wait --wait-timeout 240
done
dc logs --no-color >"$log_dir/before-reset.log"
dc down -v
export P2P_NETWORK=devnet-test BLOCK_TIME=500ms FIBRE_HOST=validator:7980
dc up -d --wait --wait-timeout 240
dc exec -T validator jq -e '.chain_id == "devnet-test"' /data/validator/config/genesis.json >/dev/null
pff --verify
dc stop validator
if dc run --rm --no-deps -e FIBRE_HOST=invalid -e STARTUP_TIMEOUT=30 validator >"$log_dir/registration-failure.log" 2>&1; then
  echo 'Invalid Fibre registration unexpectedly succeeded.' >&2
  exit 1
fi
grep -q 'host must be in host:port form' "$log_dir/registration-failure.log"
echo "Devnet smoke tests passed. Logs: $log_dir"
