#!/bin/bash

# This script enables consumers to verify signatures on artifacts.

# Check if the number of arguments is not 2
if [[ $# -ne 2 ]]; then
    echo "Error: Exactly two arguments are required."
    echo "Example usage:"
    echo "  ./verify-signature.sh <signature-file> <file-to-verify>"
    exit 1
fi

# PGP Key
# celestia-app-maintainers <celestia-app-maintainers@celestia.org>
# BF02F32CC36864560B90B764D469F859693DC3FA
KEY_FILENAME="celestia-app-maintainers.asc"
GITHUB_URL="https://raw.githubusercontent.com/celestiaorg/celestia-app/main/scripts/signing/${KEY_FILENAME}"

echo "Downloading the celestia-app-maintainers public key"
curl -L ${GITHUB_URL} -o ${KEY_FILENAME}

echo "Importing ${KEY_FILENAME}"
gpg --import ${KEY_FILENAME}

echo "Deleting ${KEY_FILENAME}"
rm ${KEY_FILENAME}

echo "Verifying the signature of "$1" with "$2""
gpg --verify $1 $2
