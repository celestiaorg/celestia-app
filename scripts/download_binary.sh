#!/bin/bash
# Usage: ./scripts/download_binary.sh <url> <out> <version>

set -euo pipefail

url=$1
out=$2
version=$3

if [ -f internal/embedding/$out ]; then
    if [ -f internal/embedding/.embed_version_$out ]; then
        existing_version=$(cat internal/embedding/.embed_version_$out)
        if [ "$existing_version" = "$version" ]; then
            echo "Skipping download because expected version already downloaded: $out"
            exit 0
        else
            echo "Downloaded binary does not match expected version so re-downloading $out"
        fi
    else
        echo "A .embed_version file was not found for $out so downloading"
    fi
else
    echo "Binary $out not found, downloading"
fi

# Retry on transient failures (connection refused, host errors, and transient
# HTTP status codes such as 429 rate limiting and 5xx server errors). A genuine
# 404 (missing asset) is not in the retry list, so it still fails fast.
wget -q \
    --tries=5 \
    --waitretry=5 \
    --retry-connrefused \
    --retry-on-host-error \
    --retry-on-http-error=429,500,502,503,504 \
    "https://github.com/celestiaorg/celestia-app/releases/download/$version/$url" \
    -O internal/embedding/$out
echo "$version" > internal/embedding/.embed_version_$out
