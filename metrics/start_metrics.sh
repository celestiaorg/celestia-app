#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
DOCKER_DIR="$SCRIPT_DIR/docker"
TARGETS_FILE="$DOCKER_DIR/targets/targets.json"

if [[ ! -f "$TARGETS_FILE" ]]; then
  echo "targets file not found at $TARGETS_FILE" >&2
  exit 1
fi

cd "$DOCKER_DIR"

docker compose up -d

echo "✅ metrics stack started"
