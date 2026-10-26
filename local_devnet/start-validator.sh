#!/usr/bin/env bash
set -euo pipefail
# The Fibre servers sign over plaintext gRPC on the compose network, so the
# privval listener binds to 0.0.0.0. The override is a start flag rather than a
# config.toml edit so existing devnet volumes pick it up too.
exec celestia-appd start --home "/data/validator-$VALIDATOR" \
  --api.enable --api.address tcp://0.0.0.0:1317 \
  --grpc.enable --grpc.address 0.0.0.0:9090 --delayed-precommit-timeout 1s --force-no-bbr \
  --privval-grpc-allow-insecure
