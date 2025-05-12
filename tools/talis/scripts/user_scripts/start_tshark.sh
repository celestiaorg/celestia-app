#!/bin/bash

# Expand the SSH key path
# Set default SSH key location
DEFAULT_SSH_KEY="~/.ssh/id_ed25519"

# Allow user to override the SSH key location
SSH_KEY=${SSH_KEY:-$DEFAULT_SSH_KEY}

TIMEOUT=60

# Fetch the IP addresses from Pulumi stack outputs
STACK_OUTPUT=$(pulumi stack output -j)
VALIDATOR_1_IP=$(echo "$STACK_OUTPUT" | jq -r '.["validator-1"]')

# Variables
USER="root"
TMUX_SESSION_NAME="tshark"
COMMAND="export DEBIAN_FRONTEND=noninteractive && apt install tshark -y && tshark -i any -f "tcp" -s 128 -w tcp_capture.pcapng"

# Function to start tmux session on a remote server
start_tmux_session() {
  local IP=$1
  {
    echo "Starting tmux session on $IP -----------------------------------------------------"
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ${SSH_KEY} ${USER}@${IP} << EOF
tmux new-session -d -s ${TMUX_SESSION_NAME}
tmux send-keys -t ${TMUX_SESSION_NAME} "${COMMAND}" C-m
EOF
    echo "Tmux session started on $IP"
  } &

  PID=$!
  (sleep $TIMEOUT && kill -HUP $PID) 2>/dev/null &

  if wait $PID 2>/dev/null; then
    echo "$IP: Tmux session started within timeout"
  else
    echo "$IP: Operation timed out"
  fi
}

# Start tmux session on the specified validator node
start_tmux_session "$VALIDATOR_1_IP"

# Wait for all background processes to finish
wait
