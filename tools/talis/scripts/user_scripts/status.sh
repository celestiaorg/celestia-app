#!/bin/bash
set +m  # Disable job control notifications

# Set default SSH key location
DEFAULT_SSH_KEY="~/.ssh/id_ed25519"
SSH_KEY=${SSH_KEY:-$DEFAULT_SSH_KEY}

# Hardcoded path to the JSON file containing node info
NODES_JSON="./payload/validators.json"

# Port to query
PORT=26657

# Validate the JSON file exists
if [ ! -f "$NODES_JSON" ]; then
  echo "Node information JSON file not found at $NODES_JSON."
  exit 1
fi

# Function to query the status of a node
query_node_status() {
  local NODE_NAME=$1
  local IP=$2
  RESPONSE=$(curl -s "http://$IP:$PORT/status")

  if [ $? -eq 0 ]; then
    LATEST_BLOCK_HEIGHT=$(echo "$RESPONSE" | jq -r '.result.sync_info.latest_block_height' 2>/dev/null)
    if [ "$LATEST_BLOCK_HEIGHT" != "null" ] && [ -n "$LATEST_BLOCK_HEIGHT" ]; then
      echo "Latest Block Height from $NODE_NAME ($IP:$PORT): $LATEST_BLOCK_HEIGHT"
    else
      echo "Failed to parse block height from $NODE_NAME ($IP:$PORT)."
    fi
  else
    echo "Failed to query $NODE_NAME ($IP:$PORT)."
  fi
}

# Parse the JSON and iterate over nodes asynchronously
while read -r NODE_NAME IP; do
  query_node_status "$NODE_NAME" "$IP" &
done < <(jq -r 'to_entries[] | "\(.key) \(.value.ip)"' "$NODES_JSON")

# Wait for all background processes to finish
wait
