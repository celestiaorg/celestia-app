#!/bin/sh

# Test x/forwarding fixes on Arabica and Mocha by state syncing to just before
# v7 activation and then block syncing through v7 to tip.
#
# Prerequisites:
#   - celestia-appd binary built from test/v7.x-forwarding-fixes branch
#     (run `make build` and add ./build/ to PATH or copy the binary)
#
# Usage:
#   ./scripts/test-forwarding-fixes-sync.sh --network <arabica|mocha> --rpc <RPC_URL>
#
# Example:
#   ./scripts/test-forwarding-fixes-sync.sh --network arabica --rpc "https://rpc1.example.com:26657,https://rpc2.example.com:26657"
#   ./scripts/test-forwarding-fixes-sync.sh --network mocha --rpc "https://celestia-testnet-rpc.itrocket.net"

set -o errexit
set -o nounset

# Default values
NODE_NAME="${NODE_NAME:-forwarding-fix-test}"
TRUST_HEIGHT_OFFSET="${TRUST_HEIGHT_OFFSET:-1000}"

usage() {
    echo "Usage: $0 --network <arabica|mocha> --rpc <RPC_URL>"
    echo ""
    echo "Options:"
    echo "  --network    Network to test (arabica or mocha)"
    echo "  --rpc        Comma-separated RPC endpoint(s) for state sync"
    echo ""
    echo "Environment variables:"
    echo "  TRUST_HEIGHT_OFFSET  How many blocks before latest to trust (default: 1000)"
    echo "  NODE_NAME            Node name for init (default: forwarding-fix-test)"
    exit 1
}

# Parse arguments
NETWORK=""
RPC_SERVERS=""
while [ $# -gt 0 ]; do
    case "$1" in
        --network)
            NETWORK=$(echo "$2" | tr '[:upper:]' '[:lower:]')
            shift 2
            ;;
        --rpc)
            RPC_SERVERS="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

if [ -z "$NETWORK" ] || [ -z "$RPC_SERVERS" ]; then
    echo "Error: --network and --rpc are required"
    usage
fi

# Network configuration
case "$NETWORK" in
    arabica)
        CHAIN_ID="arabica-11"
        SEEDS="827583022cc6ce65cf762115642258f937c954cd@validator-1.celestia-arabica-11.com:26656,74e42b39f512f844492ff09e30af23d54579b7bc@validator-2.celestia-arabica-11.com:26656,00d577159b2eb1f524ef9c37cb389c020a2c38d2@validator-3.celestia-arabica-11.com:26656,b2871b6dc2e18916d07264af0e87c456c2bba04f@validator-4.celestia-arabica-11.com:26656"
        PEERS=""
        ;;
    mocha)
        CHAIN_ID="mocha-4"
        SEEDS="b402fe40f3474e9e208840702e1b7aa37f2edc4b@celestia-testnet-seed.itrocket.net:14656"
        PEERS="daf2cecee2bd7f1b3bf94839f993f807c6b15fbf@celestia-testnet-peer.itrocket.net:11656,96b2761729cea90ee7c61206433fc0ba40c245bf@57.128.141.126:11656,f4f75a55bfc5f302ef34435ef096a4551ecb6804@152.53.33.96:12056,31bb1c9c1be7743d1115a8270bd1c83d01a9120a@148.72.141.31:26676,3e30bcfc55e7d351f18144aab4b0973e9e9bf987@65.108.226.183:11656,7a0d5818c0e5b0d4fbd86a9921f413f5e4e4ac1e@65.109.83.40:28656,43e9da043318a4ea0141259c17fcb06ecff816af@164.132.247.253:43656,5a7566aa030f7e5e7114dc9764f944b2b1324bcd@65.109.23.114:11656,c17c0cbf05e98656fee5f60fad469fc528f6d6de@65.109.25.113:11656,fb5e0b9efacc11916c58bbcd3606cbaa7d43c99f@65.108.234.84:28656,45504fb31eb97ea8778c920701fc8076e568a9cd@188.214.133.100:26656,edafdf47c443344fb940a32ab9d2067c482e59df@84.32.71.47:26656,ae7d00d6d70d9b9118c31ac0913e0808f2613a75@177.54.156.69:26656,7c841f59c35d70d9f1472d7d2a76a11eefb7f51f@136.243.69.100:43656"
        ;;
    *)
        echo "Unknown network: $NETWORK"
        usage
        ;;
esac

CELESTIA_APP_HOME="${HOME}/.celestia-app-forwarding-test"
CELESTIA_APP_VERSION=$(celestia-appd version 2>&1)

# Use the first RPC server for querying
FIRST_RPC=$(echo "$RPC_SERVERS" | cut -d',' -f1)

echo "=============================================="
echo "Forwarding Fixes Sync Test"
echo "=============================================="
echo "Network:     $NETWORK ($CHAIN_ID)"
echo "Binary:      celestia-appd $CELESTIA_APP_VERSION"
echo "Home:        $CELESTIA_APP_HOME"
echo "RPC:         $RPC_SERVERS"
echo "=============================================="
echo ""

# Clean up any previous test data
if [ -d "$CELESTIA_APP_HOME" ]; then
    read -p "Delete existing $CELESTIA_APP_HOME? [y/n] " response
    if [ "$response" != "y" ]; then
        echo "Aborting."
        exit 1
    fi
    rm -rf "$CELESTIA_APP_HOME"
fi

echo "Step 1: Initializing node..."
celestia-appd init "$NODE_NAME" --chain-id "$CHAIN_ID" --home "$CELESTIA_APP_HOME" > /dev/null 2>&1

echo "Step 2: Downloading genesis..."
celestia-appd download-genesis "$CHAIN_ID" --home "$CELESTIA_APP_HOME" > /dev/null 2>&1

echo "Step 3: Configuring seeds and peers..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" "$CELESTIA_APP_HOME/config/config.toml"
if [ -n "$PEERS" ]; then
    sed -i -e "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"$PEERS\"/;}" "$CELESTIA_APP_HOME/config/config.toml"
fi

echo "Step 4: Querying trust height and hash from RPC..."
LATEST_HEIGHT=$(curl -s "$FIRST_RPC/block" | python3 -c "import sys,json; print(json.load(sys.stdin)['result']['block']['header']['height'])")
TRUST_HEIGHT=$((LATEST_HEIGHT - TRUST_HEIGHT_OFFSET))
TRUST_HASH=$(curl -s "$FIRST_RPC/block?height=$TRUST_HEIGHT" | python3 -c "import sys,json; print(json.load(sys.stdin)['result']['block_id']['hash'])")
echo "  Latest height: $LATEST_HEIGHT"
echo "  Trust height:  $TRUST_HEIGHT"
echo "  Trust hash:    $TRUST_HASH"

echo "Step 5: Configuring state sync..."
sed -i.bak -E \
    "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
     s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC_SERVERS,$RPC_SERVERS\"| ; \
     s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$TRUST_HEIGHT| ; \
     s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" \
    "$CELESTIA_APP_HOME/config/config.toml"

echo "Step 6: Applying speed optimizations..."

# Disable tx indexing (unnecessary for sync testing)
sed -i.bak 's/^indexer *=.*/indexer = "null"/' "$CELESTIA_APP_HOME/config/config.toml"

# Set log level to info so we can see state sync and block sync progress
sed -i 's/^log_level *=.*/log_level = "info"/' "$CELESTIA_APP_HOME/config/config.toml"

# Use default pruning (aggressive pruning conflicts with state sync snapshots)
sed -i.bak 's/^pruning *=.*/pruning = "default"/' "$CELESTIA_APP_HOME/config/app.toml"

# Disable state sync verification for speed
sed -i 's/^verify_data *=.*/verify_data = false/' "$CELESTIA_APP_HOME/config/config.toml"

echo "Step 7: Starting state sync..."
echo "  The node will state sync to ~height $TRUST_HEIGHT then block sync to tip."
echo "  Watch for consensus failures, panics, or app hash mismatches."
echo "  Press Ctrl+C to stop once synced to tip."
echo ""

celestia-appd start --home "$CELESTIA_APP_HOME"
