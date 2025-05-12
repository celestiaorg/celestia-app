#!/bin/bash

# Expand the SSH key path
# Set default SSH key location
DEFAULT_SSH_KEY="~/.ssh/id_ed25519"

# Allow user to override the SSH key location
SSH_KEY=${SSH_KEY:-$DEFAULT_SSH_KEY}

TIMEOUT=60

# Fetch the IP addresses from Pulumi stack outputs
STACK_OUTPUT=$(pulumi stack output -j)
DROPLET_IPS=$(echo "$STACK_OUTPUT" | jq -r '.[]')

# Variables
USER="root"
TMUX_SESSION_NAME="txsim"
COMMAND="txsim .celestia-app/keyring-test --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000000 --key-path .celestia-app --grpc-endpoint localhost:9090 --feegrant"

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

MAX_LOOPS=100
COUNTER=0

for IP in $DROPLET_IPS; do
  start_tmux_session "$IP" &

  # Increment the counter
  COUNTER=$((COUNTER + 1))

  # Check if the counter has reached the max number of loops
  if [ "$COUNTER" -ge "$MAX_LOOPS" ]; then
    break
  fi
done

# Wait for all background processes to finish
wait

# some list of grpc endpoints
(conval-0.par.mamochain.com conval-1.par.mamochain.com conval-2.par.mamochain.com)
# some other list of key paths
(validator-0 validator-1 validator-2 validator-3)
# create the command
# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint plaintext://$END_POINT:9090 --feegrant --key-path /home/evan/cache/mamo-2/$KEY_PATH
# start a new tmux window (or session but I think a window would be nicer?)

# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-0.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-1/validator-0

# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-1.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-2/validator-1
# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-2.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-2/validator-2
# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-3.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-2/validator-3
# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-4.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-2/validator-4
# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-5.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-2/validator-5
# txsim --blob 1 --blob-amounts 1 --blob-sizes 1900000-2000001 --grpc-endpoint conval-6.par.mamochain.com:9090 --feegrant --key-path /home/evan/cache/mamo-2/validator-6
