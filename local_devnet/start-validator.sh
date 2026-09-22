#!/usr/bin/env bash
set -euo pipefail
exec celestia-appd start --home "/data/validator-$VALIDATOR" \
  --api.enable --api.address tcp://0.0.0.0:1317 \
  --grpc.enable --grpc.address 0.0.0.0:9090 --delayed-precommit-timeout 1s --force-no-bbr
