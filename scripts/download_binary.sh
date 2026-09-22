#!/bin/bash
# Usage: ./scripts/download_binary.sh <url> <out> <version>
#
# Downloads a celestia-app release archive and verifies its SHA-256 against
# scripts/embedded_checksums.txt before it is embedded in the multiplexer.

set -euo pipefail

url=$1
out=$2
version=$3

checksums_file="$(dirname "$0")/embedded_checksums.txt"
target="internal/embedding/$out"
version_file="internal/embedding/.embed_version_$out"

expected=$(awk -v key="$version/$url" '$2 == key { print $1 }' "$checksums_file")
if [ -z "$expected" ]; then
    echo "ERROR: no checksum for $version/$url in $checksums_file"
    exit 1
fi

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{ print $1 }'
    else
        shasum -a 256 "$1" | awk '{ print $1 }'
    fi
}

if [ -f "$target" ]; then
    if [ -f "$version_file" ]; then
        existing_version=$(cat "$version_file")
        if [ "$existing_version" = "$version" ] && [ "$(sha256 "$target")" = "$expected" ]; then
            echo "Skipping download because expected version already downloaded: $out"
            exit 0
        else
            echo "Downloaded binary does not match expected version or checksum so re-downloading $out"
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
    -O "$target"

actual=$(sha256 "$target")
if [ "$actual" != "$expected" ]; then
    rm -f "$target" "$version_file"
    echo "ERROR: checksum mismatch for $version/$url"
    echo "expected: $expected"
    echo "actual:   $actual"
    exit 1
fi
echo "$version" > "$version_file"
