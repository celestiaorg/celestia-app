#!/bin/sh
# Build the Corto-10 linux/arm64 binaries and print their sha256 (the S3
# content-address). See BUILD.md for the ship/install step.
set -eu
ROOT=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
cd "$ROOT"

export GOOS=linux GOARCH=arm64 CGO_ENABLED=0

make build-standalone
GOWORK=off make build-fibre-server

for bin in build/celestia-appd build/fibre; do
  sha256sum "$bin"
done
