#!/bin/bash
# Usage: ./scripts/embed_bin.sh <url> <out> <version>

set -euo pipefail

url=$1
out=$2
version=$3

if [ -f internal/embedding/$out ]; then
    if [ -f internal/embedding/.embed_version_$out ]; then
        existing_version=$(cat internal/embedding/.embed_version_$out)
        if [ "$existing_version" = "$version" ]; then
            echo "Skipping download, existing correct version: $out"
            exit 0
        else
            echo "Version mismatch, re-downloading $out"
        fi
    else
        echo "No version file for $out, re-downloading"
    fi
else
    echo "Binary $out not found, downloading"
fi

wget -q "https://github.com/celestiaorg/celestia-app/releases/download/$version/$url" -O internal/embedding/$out
echo "$version" > internal/embedding/.embed_version_$out
