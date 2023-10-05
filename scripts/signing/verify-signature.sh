#!/bin/bash

KEYS_DIR="./keys"

echo "Importing the public keys in $KEYS_DIR..."

# Loop over all keys in the keys directory
for key in "$KEYS_DIR"/*; do
    # Check if it's a regular file (and not a directory or other type)
    if [[ -f "$key" ]]; then
        # Import the key
        echo "Importing $key"
        gpg --import "$key"
    fi
done

echo "Verifying the signature of "$1" with "$2"..."
gpg --verify $1 $2
