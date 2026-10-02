#!/bin/sh
set -eu

# Retry in the same layer so successful downloads survive transient proxy errors.
attempt=1
max_attempts=5
until go mod download; do
    if [ "$attempt" -ge "$max_attempts" ]; then
        echo "go mod download failed after $max_attempts attempts" >&2
        exit 1
    fi
    echo "go mod download failed (attempt $attempt/$max_attempts); retrying after backoff..." >&2
    sleep $((attempt * 5))
    attempt=$((attempt + 1))
done
