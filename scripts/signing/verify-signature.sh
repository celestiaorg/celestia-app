#!/bin/bash

KEYS_DIR="./keys"

echo "Importing the public keys in $KEYS_DIR"

# Loop over all keys in the keys directory
for key in "$KEYS_DIR"/*; do
    # Check if it's a regular file (and not a directory or other type)
    if [[ -f "$key" ]]; then
        # Import the key
        echo "Importing $key"
        gpg --import "$key"
    fi
done

# Check if the number of arguments is not 2
if [[ $# -ne 2 ]]; then
    echo "Error: Exactly two arguments are required."
    echo "Example usage:"
    echo "  ./verify-signature.sh <signature-file> <file-to-verify>"
    exit 1
fi

echo "Verifying the signature of "$1" with "$2""
gpg --verify $1 $2
