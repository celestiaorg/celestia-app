#!/bin/bash

# Check if the correct number of arguments is provided
if [ "$#" -ne 2 ]; then
    echo "Usage: source script.sh <name> <file_type>.jsonl"
    return 1
fi

# Get the name and file type from the arguments
NAME="$1"
FILE_TYPE="$2"

# Set default SSH key location
DEFAULT_SSH_KEY="~/.ssh/id_ed25519"

# Allow user to override the SSH key location
SSH_KEY=${SSH_KEY:-$DEFAULT_SSH_KEY}


# Fetch the IP addresses from Pulumi stack outputs and store in a JSON file
pulumi stack output -j > ./payload/ips.json

# Get the IP address corresponding to the provided name
IP_ADDRESS=$(jq -r --arg NAME "$NAME" '.[$NAME]' ./payload/ips.json)

# Check if the IP address was found
if [ -z "$IP_ADDRESS" ]; then
    echo "No IP address found for name: $NAME"
    return 1
fi

# Define the source file path on the remote server
REMOTE_FILE="root@$IP_ADDRESS:/root/.celestia-app/data/traces/$FILE_TYPE"

# Define the destination file path
DEST_FILE="./$FILE_TYPE"

# Download the file from the remote server to the current directory
scp -i "$SSH_KEY" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$REMOTE_FILE" "$DEST_FILE"

# Check if the scp operation was successful
if [ $? -eq 0 ]; then
    echo "File $FILE_TYPE downloaded successfully to the current directory."
else
    echo "Failed to download the file."
    return 1
fi
