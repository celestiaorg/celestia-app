#!/usr/bin/env bash
set -euo pipefail
if [[ -f /data/initialized ]]; then
  echo 'Using existing devnet data.'
  exit 0
fi
if [[ -d /data/validator-1 ]]; then
  echo 'Incomplete initialization. Run docker compose down -v to reset this devnet.' >&2
  exit 1
fi
umask 077
quiet() { "$@" >/data/init.log 2>&1 || { cat /data/init.log >&2; exit 1; }; }
for n in 1 2; do
  home=/data/validator-$n
  quiet celestia-appd init "validator-$n" --chain-id local-devnet --home "$home"
  celestia-appd keys add validator --home "$home" --keyring-backend test --output json >"$home/account.json"
done
celestia-appd keys add test --home /data/test --keyring-backend test --output json >/data/test-account.json
for account in /data/validator-{1,2}/account.json /data/test-account.json; do
  celestia-appd genesis add-genesis-account "$(jq -r .address "$account")" 1000000000000000utia --home /data/validator-1
done
cp /data/validator-1/config/genesis.json /data/validator-2/config/genesis.json
for n in 1 2; do
  quiet celestia-appd genesis gentx validator 5000000000utia --home "/data/validator-$n" \
    --keyring-backend test --chain-id local-devnet --ip "validator-$n" --fees 5000utia
done
cp /data/validator-2/config/gentx/*.json /data/validator-1/config/gentx/
quiet celestia-appd genesis collect-gentxs --home /data/validator-1
jq -e '.consensus.params.version.app == "10"' /data/validator-1/config/genesis.json
cp /data/validator-1/config/genesis.json /data/validator-2/config/genesis.json
id1=$(celestia-appd comet show-node-id --home /data/validator-1)
id2=$(celestia-appd comet show-node-id --home /data/validator-2)
for n in 1 2; do
  config=/data/validator-$n/config/config.toml
  peer="$id1@validator-1:26656"
  [[ "$n" == 1 ]] && peer="$id2@validator-2:26656"
  sed -i \
    -e 's#tcp://127.0.0.1:26657#tcp://0.0.0.0:26657#g' \
    -e 's/^priv_validator_grpc_laddr = .*/priv_validator_grpc_laddr = "0.0.0.0:26669"/' \
    -e "s/^persistent_peers = .*/persistent_peers = \"$peer\"/" \
    -e 's/^addr_book_strict = true/addr_book_strict = false/' \
    -e 's/^allow_duplicate_ip = false/allow_duplicate_ip = true/' \
    -e 's/^indexer = "null"/indexer = "kv"/' \
    -e 's/^discard_abci_responses = true/discard_abci_responses = false/' "$config"
done
touch /data/initialized
echo 'Initialized two v10 validators and funded account test.'
