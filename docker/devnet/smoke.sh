#!/usr/bin/env bash
set -euo pipefail
dir=$(dirname "$(realpath "$0")")
export DEVNET_VERSION=${DEVNET_VERSION:-local}
project=celestia-devnet-test-$$
log_dir=${DEVNET_LOG_DIR:-/tmp/$project}
mkdir -p "$log_dir"
dc() { docker compose -p "$project" -f "$dir/compose.yaml" -f "$dir/compose.test.yaml" "$@"; }
other() { docker compose -p "$project-other" -f "$dir/compose.yaml" -f "$dir/compose.test.yaml" "$@"; }
cleanup() {
  dc logs --no-color >"$log_dir/final.log" 2>&1
  other logs --no-color >"$log_dir/other.log" 2>&1
  dc down -v --remove-orphans
  other down -v --remove-orphans
}
trap cleanup EXIT
trap 'echo "Smoke test failed at line $LINENO; logs: $log_dir" >&2' ERR
bridge() { dc exec -T bridge celestia "$@" --url http://127.0.0.1:26658 --token skip-auth; }
height() { dc exec -T validator curl -fsS http://127.0.0.1:26657/status | jq -er '.result.sync_info.latest_block_height | tonumber'; }
hash() { dc exec -T validator curl -fsS 'http://127.0.0.1:26657/block?height=1' | jq -er .result.block_id.hash; }
fibre() { dc exec -T validator submit --grpc 127.0.0.1:9090 --keyring-dir /data/validator --key node-0 --validators 1 --kind pff "$@"; }
start=$SECONDS
dc up -d --wait --wait-timeout 240
echo "Cached-image startup: $((SECONDS - start))s"
[[ $(dc ps -q | wc -l | tr -d ' ') == 2 ]]
curl -fsS "http://$(dc port validator 26657)/status" | jq -e '.result.node_info.network == "devnet"' >/dev/null
address=$(dc exec -T validator cat /credentials/node-0.addr)
# shellcheck disable=SC2016
credentials=$(dc exec -T validator bash -c 'sha256sum /credentials/*')
# shellcheck disable=SC2016
[[ $(dc exec -T bridge bash -c 'sha256sum /credentials/*') == "$credentials" ]]
initial_hash=$(hash)
# shellcheck disable=SC2016
dc exec -T validator bash -c '
  for name in validator-0 node-{0..9}; do
    address=$(cat "/credentials/$name.addr")
    jq -e --arg address "$address" '\''.app_state.bank.balances[] | select(.address == $address) | .coins == [{denom: "utia", amount: "1000000000000000"}]'\'' /data/validator/config/genesis.json >/dev/null
    celestia-appd query fibre escrow-account "$address" --home /data/validator --node tcp://127.0.0.1:26657 --output json |
      jq -e '\''.found and .escrow_account.balance.amount == "1000000000000" and .escrow_account.available_balance.amount == "1000000000000"'\'' >/dev/null
  done
  for i in {0..9}; do
    celestia-appd query bank balances "$(cat "/credentials/node-$i.addr")" --home /data/validator --node tcp://127.0.0.1:26657 --output json |
      jq -e '\''.balances == [{denom: "utia", amount: "1000000000000000"}]'\'' >/dev/null
  done
'
receipt=$(bridge blob submit 0x0102030405060708090a 0x6465766e6574)
blob_height=$(jq -er '.result.height' <<<"$receipt")
commitment=$(jq -er '.result.commitments[0]' <<<"$receipt")
bridge blob get "$blob_height" 0x0102030405060708090a "$commitment" | jq -e '.result.data == "devnet"' >/dev/null
fibre --size 262144 --verify --save /data/receipt.json
initial_height=$(height)
dc stop
for service in validator bridge; do
  [[ $(docker inspect -f '{{.State.ExitCode}}' "$(dc ps -aq "$service")") == 0 ]]
done
dc up -d --wait --wait-timeout 240
[[ $(hash) == "$initial_hash" && $(height) -gt "$initial_height" ]]
bridge blob get "$blob_height" 0x0102030405060708090a "$commitment" | jq -e '.result.data == "devnet"' >/dev/null
fibre --read /data/receipt.json
other up -d --wait --wait-timeout 240
[[ $(other exec -T validator cat /credentials/node-0.addr) == "$address" ]]
[[ $(other exec -T validator curl -fsS 'http://127.0.0.1:26657/block?height=1' | jq -er .result.block_id.hash) != "$initial_hash" ]]
other down -v
for process in fibre celestia-appd; do
  dc logs --no-color >"$log_dir/before-$process-failure.log"
  # shellcheck disable=SC2016
  dc exec -T validator bash -c 'for file in /proc/[0-9]*/comm; do if [[ $(cat "$file" 2>/dev/null) == "$1" ]]; then pid=${file#/proc/}; kill -KILL "${pid%/comm}"; fi; done' bash "$process"
  id=$(dc ps -aq validator)
  for ((attempt=0; attempt<30; attempt++)); do
    [[ $(docker inspect -f '{{.State.Running}}' "$id") == true ]] || break
    sleep 1
  done
  [[ $(docker inspect -f '{{.State.Running}}' "$id") == false ]]
  [[ $(docker inspect -f '{{.State.ExitCode}}' "$id") != 0 ]]
  dc logs --no-color >"$log_dir/$process-failure.log"
  dc down
  dc up -d --wait --wait-timeout 240
done
dc logs --no-color >"$log_dir/before-reset.log"
dc down -v
export P2P_NETWORK=devnet-test BLOCK_TIME=500ms FIBRE_HOST=validator:7980
dc up -d --wait --wait-timeout 240
[[ $(dc exec -T validator cat /credentials/node-0.addr) == "$address" ]]
# shellcheck disable=SC2016
[[ $(dc exec -T validator bash -c 'sha256sum /credentials/*') == "$credentials" ]]
[[ $(hash) != "$initial_hash" ]]
dc exec -T validator jq -e '.chain_id == "devnet-test"' /data/validator/config/genesis.json >/dev/null
fibre --verify
dc logs --no-color >"$log_dir/overrides.log"
network=$(docker inspect -f '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{end}}' "$(dc ps -q bridge)")
for setting in CORE_HOST=missing CORE_GRPC_PORT=1; do
  failure_start=$SECONDS
  if docker run --rm --network "$network" -e P2P_NETWORK -e "$setting" -e STARTUP_TIMEOUT=5 "ghcr.io/celestiaorg/celestia-devnet-bridge:$DEVNET_VERSION" >"$log_dir/$setting.log" 2>&1; then
    echo "Bridge unexpectedly started with $setting." >&2
    exit 1
  fi
  ((SECONDS - failure_start < 30))
  echo "Rejected $setting in $((SECONDS - failure_start))s"
  grep -Eq 'Timed out waiting|failed to synchronize|connection refused|context deadline exceeded' "$log_dir/$setting.log"
done
dc stop bridge validator
if dc run --rm --no-deps -e FIBRE_HOST=invalid -e STARTUP_TIMEOUT=5 validator >"$log_dir/registration-failure.log" 2>&1; then
  echo 'Invalid Fibre registration unexpectedly succeeded.' >&2
  exit 1
fi
grep -q 'host must be in host:port form' "$log_dir/registration-failure.log"
echo "Devnet smoke tests passed. Logs: $log_dir"
