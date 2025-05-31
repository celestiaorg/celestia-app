#!/bin/bash

# Expand the SSH key path
DEFAULT_SSH_KEY="~/.ssh/id_ed25519"
SSH_KEY=${SSH_KEY:-$DEFAULT_SSH_KEY}

# Timeout for operations
TIMEOUT=60

# Fetch the IP addresses from Pulumi stack outputs
STACK_OUTPUT=$(pulumi stack output -j)
DROPLET_IPS=$(echo "$STACK_OUTPUT" | jq -r '.[]')

# Variables
USER="root"

# Function to kill processes on port 26657 on a remote server
kill_port_26657() {
  local IP=$1
  {
    echo "Processing $IP -----------------------"
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ${SSH_KEY} ${USER}@${IP} << EOF
kill -9 \$(lsof -t -i :26657) 2>/dev/null
if [ \$? -eq 0 ]; then
  echo "Processes on port 26657 killed on $IP"
else
  echo "No processes found on port 26657 on $IP"
fi
EOF
  } &

  PID=$!
  (sleep $TIMEOUT && kill -HUP $PID) 2>/dev/null &

  if wait $PID 2>/dev/null; then
    echo "$IP: Port 26657 management completed within timeout"
  else
    echo "$IP: Operation timed out"
  fi
}

# Loop through IP addresses and perform actions asynchronously
for IP in $DROPLET_IPS; do
  kill_port_26657 "$IP" &
done

# Wait for all background processes to finish
wait

