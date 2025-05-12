#!/bin/bash

# Ensure two arguments are passed
if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <tmux-session-name> <script-name>"
fi

# Arguments
TMUX_SESSION_NAME=$1
SCRIPT_NAME=$2
PAYLOAD_PATH="/root/payload"

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

# Function to start tmux session on a remote server
start_tmux_session() {
  local IP=$1
  {
    echo "Starting tmux session on $IP -----------------------------------------------------"
    ssh -t -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ${SSH_KEY} ${USER}@${IP} << EOF
tmux new-session -d -s ${TMUX_SESSION_NAME}
tmux send-keys -t ${TMUX_SESSION_NAME} "bash ${PAYLOAD_PATH}/${SCRIPT_NAME}" C-m
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

# Loop through IP addresses and trigger the tmux session asynchronously
for IP in $DROPLET_IPS; do
  start_tmux_session "$IP" &
done

# Wait for all background processes to finish
wait
