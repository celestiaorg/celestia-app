#!/bin/bash
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

apt install -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" apt-transport-https ca-certificates gnupg curl -y

# ensure that the env vars are exported here
source /root/payload/vars.sh
echo "CHAIN_ID after sourcing vars.sh: $CHAIN_ID"

# Set environment variables
PROJECT_ID="numeric-mile-433416-e9"
DATASET_ID="traces"

CHAIN_ID=$CHAIN_ID

LOCAL_DIR="/root/.celestia-app/data/traces"

tmux kill-session -t app

# Get the hostname
hostname=$(hostname)

# Parse the first part of the hostname
nodeID=$(echo $hostname | awk -F'-' '{print $1 "-" $2}')

source_dir="/root/.celestia-app/data/traces"
logs_path="/root/logs"

# clean the data by removing the last line
find $source_dir -type f -name "*.jsonl" -exec sed -i '$d' {} \;

AWS_DEFAULT_REGION="us-east-2"
S3_BUCKET_NAME="block-prop-traces-ef"
echo "All files loaded."

snap install aws-cli --classic
destination_file="/tmp/${CHAIN_ID}_${nodeID}_traces.tar.gz"

# Set the base S3 path
base_s3_path="s3://${S3_BUCKET_NAME}/${CHAIN_ID}/${nodeID}/"

# Upload the directory structure to S3
aws s3 cp "$source_dir" "$base_s3_path" --recursive --region $AWS_DEFAULT_REGION
aws s3 cp "$logs_path" "$base_s3_path" --region $AWS_DEFAULT_REGION
