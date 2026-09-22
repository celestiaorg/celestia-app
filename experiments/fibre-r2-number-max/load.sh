#!/usr/bin/env bash
# Starts PROCS fibre-txsim processes with CONCURRENCY workers each for DURATION.
# Each process opens its own connection to every validator and uses its own keys.
# Usage: ./load.sh <procs> <concurrency> <duration> <app-grpc-endpoint> [tag]
set -euo pipefail
PROCS="$1"; CONCURRENCY="$2"; DURATION="$3"; ENDPOINT="$4"; TAG="${5:-load}"
BLOB_SIZE="${BLOB_SIZE:-1342177275}"
mkdir -p "/root/logs/$TAG"
for p in $(seq 0 $((PROCS - 1))); do
  tmux new-session -d -s "txsim-$p" \
    "GOMEMLIMIT=${GOMEMLIMIT:-off} fibre-txsim --chain-id talis-fibre-r2 --grpc-endpoint $ENDPOINT --keyring-dir encoder-payload/encoder-0 \
      --key-prefix enc0 --key-offset $((p * CONCURRENCY)) --concurrency $CONCURRENCY \
      --blob-size $BLOB_SIZE --experimental-max-blob-size-mib 1280 --duration $DURATION \
      > /root/logs/$TAG/txsim-$p.log 2>&1"
done
echo "started $PROCS x $CONCURRENCY workers for $DURATION ($TAG)"
