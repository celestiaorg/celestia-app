#!/bin/bash

DIRECTORY_TO_TRANSFER="./payload"
ARCHIVE_NAME="payload.tar.gz"
# Set default SSH key location
DEFAULT_SSH_KEY="~/.ssh/id_ed25519"
# Allow user to override the SSH key location
SSH_KEY=${SSH_KEY:-$DEFAULT_SSH_KEY}

# Fetch the IP addresses from Pulumi stack outputs
pulumi stack output -j > ./payload/ips.json
STACK_OUTPUT=$(pulumi stack output -j)
echo $STACK_OUTPUT
DROPLET_IPS=$(echo "$STACK_OUTPUT" | jq -r '.[]')

# Function to transfer and uncompress files on the remote server
transfer_and_uncompress() {
  local IP=$1
  echo "Transferring files to $IP -----------------------"
  scp -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$ARCHIVE_NAME" "root@$IP:/root/"

  # Uncompress the directory on the remote node
  echo "Uncompressing the directory on $IP..."
  ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "root@$IP" "tar -xzf /root/$ARCHIVE_NAME -C /root/"
}

# Loop through the IPs and run the transfer and uncompress in parallel
for IP in $DROPLET_IPS; do
  transfer_and_uncompress "$IP" &
done

# Wait for all background processes to finish
wait

# Cleanup local archive
rm "$ARCHIVE_NAME"
