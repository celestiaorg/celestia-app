#!/usr/bin/env bash
set -euo pipefail

cd "${1:-.}"
for attempt in 1 2 3; do
  if timeout 3m go mod download; then
    exit 0
  fi
  if [ "$attempt" -lt 3 ]; then
    echo "Module download failed (attempt $attempt/3); retrying..." >&2
    sleep "$((attempt * 5))"
  fi
done

echo "Module download failed after 3 attempts." >&2
exit 1
