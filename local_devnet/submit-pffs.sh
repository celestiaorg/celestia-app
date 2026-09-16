#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
exec docker compose run --rm --no-deps tools --kind pff "$@"
