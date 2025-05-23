#!/bin/bash

# Ensure one argument is passed
if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <tmux-session-name>"
  exit 1
fi

# Argument
TMUX_SESSION_NAME=$1

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

# Function to kill the tmux session on a remote server
kill_tmux_session() {
  local IP=$1
  {
    echo "Attempting to kill tmux session '$TMUX_SESSION_NAME' on $IP -----------------------"
    ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i ${SSH_KEY} ${USER}@${IP} << EOF
tmux has-session -t ${TMUX_SESSION_NAME} 2>/dev/null
if [ \$? -eq 0 ]; then
  tmux kill-session -t ${TMUX_SESSION_NAME}
  echo "Tmux session '$TMUX_SESSION_NAME' killed on $IP"
else
  echo "No tmux session named '$TMUX_SESSION_NAME' found on $IP"
fi
EOF
  } &

  PID=$!
  (sleep $TIMEOUT && kill -HUP $PID) 2>/dev/null &

  if wait $PID 2>/dev/null; then
    echo "$IP: Tmux session management completed within timeout"
  else
    echo "$IP: Operation timed out"
  fi
}

# Loop through IP addresses and kill the tmux session asynchronously
for IP in $DROPLET_IPS; do
  kill_tmux_session "$IP" &
done

# Wait for all background processes to finish
wait
