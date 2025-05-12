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

cp ./scripts/init_install.sh ./payload/init_install.sh
cp ./scripts/txsim.sh ./payload/txsim.sh
cp ./scripts/vars.sh ./payload/vars.sh
cp ./scripts/upload_traces.sh ./payload/upload_traces.sh
cp ./scripts/shutdown_txsim.sh ./payload/shutdown_txsim.sh
cp ./scripts/debug_install.sh ./payload/debug_install.sh
cp /home/evan/go/src/github.com/celestiaorg/celestia-app/build/celestia-appd ./payload/celestia-appd
cp /home/evan/go/src/github.com/celestiaorg/celestia-app/build/txsim ./payload/txsim



# copy the env vars into a temp file that is included in the payload to each validator 
echo "export CHAIN_ID=\"$CHAIN_ID\"" >> ./payload/vars.sh
echo "export AWS_DEFAULT_REGION=\"$AWS_DEFAULT_REGION\"" >> ./payload/vars.sh
echo "export AWS_ACCESS_KEY_ID=\"$AWS_ACCESS_KEY_ID\"" >> ./payload/vars.sh
echo "export AWS_SECRET_ACCESS_KEY=\"$AWS_SECRET_ACCESS_KEY\"" >> ./payload/vars.sh
echo "export S3_BUCKET_NAME=\"$S3_BUCKET_NAME\"" >> ./payload/vars.sh

# sleep 30

# Compress the directory
echo "Compressing the directory $DIRECTORY_TO_TRANSFER..."
tar -czf "$ARCHIVE_NAME" -C "$(dirname "$DIRECTORY_TO_TRANSFER")" "$(basename "$DIRECTORY_TO_TRANSFER")"

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
