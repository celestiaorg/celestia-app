#!/usr/bin/env bash
set -euo pipefail
umask 077
home=${CELESTIA_APP_HOME:-/data/validator}
network=${P2P_NETWORK:-devnet}
app() { celestia-appd --home "$home" "$@"; }
if [[ ! -f "$home/initialized" ]]; then
  if [[ -f "$home/config/genesis.json" ]]; then
    echo 'Incomplete initialization; reset the devnet volumes.' >&2
    exit 1
  fi
  app init validator --chain-id "$network"
  addresses=()
  for name in validator-0 node-{0..9}; do
    app keys import-hex "$name" "$(cat "/credentials/$name.plaintext-key")" --keyring-backend test
    address=$(cat "/credentials/$name.addr")
    addresses+=("$address")
    app genesis add-genesis-account "$address" 1000000000000000utia
  done
  module=$(app debug addr "$(printf fibre | sha256sum | cut -c 1-40)" | awk '/Bech32 Acc:/ {print $3}')
  app genesis add-genesis-account "$module" 11000000000000utia --module-name fibre
  genesis=$home/config/genesis.json
  jq --args '
    {denom: "utia", amount: "1000000000000"} as $balance |
    .app_state.fibre.escrow_accounts = [
      $ARGS.positional[] | {signer: ., balance: $balance, available_balance: $balance}
    ] |
    (.app_state.auth.accounts[] | select(.name? == "fibre") | .permissions) = []
  ' "${addresses[@]}" <"$genesis" >"$genesis.tmp"
  mv "$genesis.tmp" "$genesis"
  app genesis gentx validator-0 5000000000utia --keyring-backend test --chain-id "$network" --fees 5000utia
  app genesis collect-gentxs
  sed -i \
    -e 's#tcp://127.0.0.1:26657#tcp://0.0.0.0:26657#g' \
    -e 's/^priv_validator_grpc_laddr = .*/priv_validator_grpc_laddr = "127.0.0.1:26669"/' \
    -e 's/^indexer = "null"/indexer = "kv"/' \
    -e 's/^discard_abci_responses = true/discard_abci_responses = false/' "$home/config/config.toml"
  touch "$home/initialized"
fi
jq -e --arg network "$network" '.chain_id == $network' "$home/config/genesis.json" >/dev/null
pids=()
# shellcheck disable=SC2329
cleanup() {
  kill -TERM "${pids[@]}" 2>/dev/null || true
  for pid in "${pids[@]}"; do wait "$pid" || true; done
}
trap cleanup EXIT
trap 'exit 0' TERM INT
celestia-appd start --home "$home" --force-no-bbr \
  --api.enable --api.address tcp://0.0.0.0:1317 --grpc.enable --grpc.address 0.0.0.0:9090 \
  --delayed-precommit-timeout "${BLOCK_TIME:-1s}" "$@" &
app_pid=$!
pids+=("$app_pid")
deadline=$((SECONDS + ${STARTUP_TIMEOUT:-120}))
until nc -z 127.0.0.1 9090 && nc -z 127.0.0.1 26669 &&
  curl --max-time 2 -fsS http://127.0.0.1:26657/status | jq -e '.result.sync_info.latest_block_height | tonumber > 1' >/dev/null; do
  if ! kill -0 "$app_pid" || ((SECONDS >= deadline)); then
    echo 'Validator failed to start gRPC services and produce blocks.' >&2
    exit 1
  fi
  sleep 1
done
fibre start --home /data/fibre --app-grpc-address 127.0.0.1:9090 \
  --signer-grpc-address 127.0.0.1:26669 --server-listen-address 0.0.0.0:7980 &
fibre_pid=$!
pids+=("$fibre_pid")
timeout "${STARTUP_TIMEOUT:-120}" bash /opt/devnet/provision.sh &
provision_pid=$!
pids+=("$provision_pid")
wait -n -p finished "${pids[@]}"
[[ $finished == "$provision_pid" ]] || exit 1
pids=("$app_pid" "$fibre_pid")
echo 'Validator and Fibre ready.'
wait -n "${pids[@]}"
exit 1
