#!/usr/bin/env bash
set -euo pipefail
trap 'echo "Fibre registration interrupted or timed out." >&2; exit 1' TERM
app() { celestia-appd --home "${CELESTIA_APP_HOME:-/data/validator}" --node tcp://127.0.0.1:26657 "$@"; }
until nc -z 127.0.0.1 7980; do sleep 1; done
if bash /opt/devnet/health.sh; then exit 0; fi
result=$(app tx valaddr set-host "${FIBRE_HOST:-localhost:7980}" --from validator-0 \
  --chain-id "${P2P_NETWORK:-devnet}" --keyring-backend test --fees 5000utia --gas 200000 --yes --output json)
jq -e '(.code // 0) == 0' <<<"$result" >/dev/null || { echo "$result" >&2; exit 1; }
hash=$(jq -er .txhash <<<"$result")
until result=$(app query tx "$hash" --output json 2>/dev/null); do sleep 1; done
jq -e '.code == 0 and (.height | tonumber > 0)' <<<"$result" >/dev/null || { echo "$result" >&2; exit 1; }
bash /opt/devnet/health.sh
