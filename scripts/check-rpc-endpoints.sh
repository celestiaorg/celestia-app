#!/bin/sh

# Script to query block_results from multiple archive RPC providers
# This is a wrapper that runs the Go tool

set -o errexit
set -o nounset

# Default values
HEIGHT="${1:-1034505}"

echo "Querying block_results at height ${HEIGHT} from multiple archive RPC providers"
echo ""

# Run the Go tool
go run "$(dirname "$0")/../tools/rpc-checker" --height "${HEIGHT}"
