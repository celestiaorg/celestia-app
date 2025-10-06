#!/bin/bash

# Usage: ./submit-slashing-proposal.sh [--home <path>] [--node <rpc-url>]
# Examples:
#   ./submit-slashing-proposal.sh
#   ./submit-slashing-proposal.sh --home ~/.celestia-app
#   ./submit-slashing-proposal.sh --node tcp://localhost:26657
#   ./submit-slashing-proposal.sh --home ~/.celestia-app --node tcp://remote-host:26657

# Parse arguments
APP_HOME="${HOME}/.celestia-app"
NODE_FLAG="--node http://public-celestia-mocha4-consensus.numia.xyz:26657"

while [[ $# -gt 0 ]]; do
  case $1 in
    --home)
      APP_HOME="$2"
      shift 2
      ;;
    --node)
      NODE_FLAG="--node $2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--home <path>] [--node <rpc-url>]"
      echo ""
      echo "Submit a governance proposal to update slashing min_signed_per_window to 1%"
      echo ""
      echo "Options:"
      echo "  --home <path>      Path to celestia-app home directory (default: ~/.celestia-app)"
      echo "  --node <rpc-url>   RPC node to connect to (default: uses local node)"
      echo "  -h, --help         Show this help message"
      echo ""
      echo "Examples:"
      echo "  $0"
      echo "  $0 --home ~/.celestia-app"
      echo "  $0 --node tcp://localhost:26657"
      echo "  $0 --home ~/.celestia-app --node tcp://remote-host:26657"
      return 0
      ;;
    *)
      # If no flag, treat as home directory (for backwards compatibility)
      APP_HOME="$1"
      shift
      ;;
  esac
done

# Configuration (matching single-node.sh)
CHAIN_ID="mocha-4"
KEY_NAME="pomifer"
KEYRING_BACKEND="test"
GAS_PRICES="0.004utia"
DEPOSIT_AMOUNT="10000000000utia"

echo "==> Submitting slashing params proposal..."
echo "    Key: $KEY_NAME"
echo "    Chain: $CHAIN_ID"
if [ -n "$NODE_FLAG" ]; then
  echo "    Node: ${NODE_FLAG#--node }"
else
  echo "    Node: local"
fi

# 1) Find the gov module authority address
echo "==> Fetching gov module authority..."
AUTHORITY=$(celestia-appd q auth module-account gov \
  --home "${APP_HOME}" \
  --chain-id "${CHAIN_ID}" \
  ${NODE_FLAG} \
  -o json 2>/dev/null | jq -r '.account.value.address // .account.base_account.address // .base_account.address // .address')

# If query failed or returned null, use the predictable gov module address
if [ -z "$AUTHORITY" ] || [ "$AUTHORITY" = "null" ]; then
  echo "==> Could not query authority, using default gov module address"
  AUTHORITY="celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7"
fi
echo "    Authority: $AUTHORITY"

# 2) Fetch current slashing params
echo "==> Fetching current slashing params..."
celestia-appd q slashing params \
  --home "${APP_HOME}" \
  --chain-id "${CHAIN_ID}" \
  ${NODE_FLAG} \
  -o json > /tmp/slashing_params.json

# 3) Build proposal.json with min_signed_per_window = 0.01
echo "==> Building proposal JSON..."
UPDATED_PARAMS=$(jq '
  if (.params.min_signed_per_window | type) == "object"
    then .params.min_signed_per_window.dec = "0.010000000000000000" | .params
    else .params.min_signed_per_window      = "0.010000000000000000" | .params
  end
' /tmp/slashing_params.json)

cat > /tmp/proposal.json <<EOF
{
  "messages": [
    {
      "@type": "/cosmos.slashing.v1beta1.MsgUpdateParams",
      "authority": "${AUTHORITY}",
      "params": ${UPDATED_PARAMS}
    }
  ],
  "metadata": "$(echo -n '{"title":"Reduce min_signed_per_window to 1%","authors":["you"],"summary":"Set slashing.min_signed_per_window to 0.01 (1%)","details":"Updates slashing params via MsgUpdateParams."}' | base64 -w0)",
  "deposit": "${DEPOSIT_AMOUNT}",
  "title": "Reduce min_signed_per_window to 1%",
  "summary": "Set slashing.min_signed_per_window to 0.01 (1%)",
  "expedited": false
}
EOF

echo "==> Submitting proposal..."
SUBMIT_OUTPUT=$(celestia-appd tx gov submit-proposal /tmp/proposal.json \
  --from "$KEY_NAME" \
  --keyring-backend "$KEYRING_BACKEND" \
  --home "${APP_HOME}" \
  --chain-id "$CHAIN_ID" \
  ${NODE_FLAG} \
  --gas auto \
  --gas-adjustment 1.3 \
  --gas-prices "$GAS_PRICES" \
  -y \
  -o json)

echo "$SUBMIT_OUTPUT" | jq .

# Check if transaction failed
TX_CODE=$(echo "$SUBMIT_OUTPUT" | jq -r '.code // 0')
if [ "$TX_CODE" != "0" ]; then
  echo "==> ERROR: Transaction failed with code $TX_CODE"
  echo "$SUBMIT_OUTPUT" | jq -r '.raw_log // .log // "Unknown error"'
  return 1
fi

# Extract proposal ID from transaction
TX_HASH=$(echo "$SUBMIT_OUTPUT" | jq -r '.txhash')
echo "==> TX Hash: $TX_HASH"

# Try to extract proposal ID from events in the response first
PROPOSAL_ID=$(echo "$SUBMIT_OUTPUT" | jq -r '.events[]? | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value' 2>/dev/null)

# If not found, wait and query the transaction
if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" = "null" ]; then
  echo "==> Waiting 2s for transaction to be indexed..."
  sleep 2

  PROPOSAL_ID=$(celestia-appd q tx "$TX_HASH" \
    --home "${APP_HOME}" \
    --chain-id "${CHAIN_ID}" \
    ${NODE_FLAG} \
    -o json 2>/dev/null | jq -r '.events[] | select(.type=="submit_proposal") | .attributes[] | select(.key=="proposal_id") | .value')
fi

if [ -z "$PROPOSAL_ID" ] || [ "$PROPOSAL_ID" = "null" ]; then
  echo "==> ERROR: Could not extract proposal ID from transaction"
  celestia-appd q tx "$TX_HASH" --home "${APP_HOME}" --chain-id "${CHAIN_ID}" ${NODE_FLAG}
  return 1
fi

echo "==> Proposal ID: $PROPOSAL_ID"

# Vote on proposal
echo "==> Voting YES on proposal $PROPOSAL_ID..."
VOTE_OUTPUT=$(celestia-appd tx gov vote "$PROPOSAL_ID" yes \
  --from "$KEY_NAME" \
  --keyring-backend "$KEYRING_BACKEND" \
  --home "${APP_HOME}" \
  --chain-id "$CHAIN_ID" \
  ${NODE_FLAG} \
  --gas auto \
  --gas-adjustment 1.5 \
  --gas-prices "$GAS_PRICES" \
  -y \
  -o json)

echo "$VOTE_OUTPUT" | jq .

VOTE_CODE=$(echo "$VOTE_OUTPUT" | jq -r '.code // 0')
if [ "$VOTE_CODE" != "0" ]; then
  echo "==> ERROR: Vote failed with code $VOTE_CODE"
  echo "$VOTE_OUTPUT" | jq -r '.raw_log // .log // "Unknown error"'
  return 1
fi

VOTE_TX_HASH=$(echo "$VOTE_OUTPUT" | jq -r '.txhash')
echo "==> Vote TX Hash: $VOTE_TX_HASH"
echo "==> Waiting 2s for vote to be indexed..."
sleep 2

# Verify the vote was recorded
echo "==> Verifying vote was recorded..."
VOTER_ADDRESS=$(celestia-appd keys show "$KEY_NAME" -a --keyring-backend "$KEYRING_BACKEND" --home "${APP_HOME}")
VOTE_CHECK=$(celestia-appd q gov vote "$PROPOSAL_ID" "$VOTER_ADDRESS" \
  --home "${APP_HOME}" \
  --chain-id "${CHAIN_ID}" \
  ${NODE_FLAG} \
  -o json 2>&1)

if echo "$VOTE_CHECK" | grep -q "not found"; then
  echo "==> ERROR: Vote was not recorded!"
  echo "==> Checking vote transaction..."
  celestia-appd q tx "$VOTE_TX_HASH" --home "${APP_HOME}" --chain-id "${CHAIN_ID}" ${NODE_FLAG}
  return 1
else
  echo "==> Vote successfully recorded!"
  echo "$VOTE_CHECK" | jq -r '.vote.options'
fi

echo ""
echo "==> Done! Query proposal status with:"
if [ -n "$NODE_FLAG" ]; then
  echo "    celestia-appd q gov proposal $PROPOSAL_ID --home ${APP_HOME} --chain-id ${CHAIN_ID} ${NODE_FLAG}"
else
  echo "    celestia-appd q gov proposal $PROPOSAL_ID --home ${APP_HOME} --chain-id ${CHAIN_ID}"
fi
