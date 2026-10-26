#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
docker compose up -d --build --wait --wait-timeout 240 validator-1 validator-2 fibre-1 fibre-2
docker compose run --rm --no-deps setup
