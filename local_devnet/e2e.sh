#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
./up.sh
docker compose run --rm --no-deps --entrypoint bash tools /opt/devnet/check.sh
./submit-pfbs.sh --count 3 --size 1024 --interval 100ms
./submit-pffs.sh --count 3 --size 1024 --interval 100ms --verify
./submit-pfbs.sh --size 262144
./submit-pffs.sh --size 262144 --verify
for port in 26657 26658; do
  curl -fsS "http://127.0.0.1:$port/status" >/dev/null
done
for port in 1317 1318; do
  curl -fsS "http://127.0.0.1:$port/cosmos/base/tendermint/v1beta1/node_info" >/dev/null
done
echo 'E2E passed. The devnet is still running.'
