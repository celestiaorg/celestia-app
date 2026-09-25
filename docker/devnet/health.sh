#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == bridge ]]; then
  height=$(curl --max-time 2 -fsS "http://${CORE_HOST:-validator}:${CORE_RPC_PORT:-26657}/status" | jq -er '.result.sync_info.latest_block_height | tonumber')
  ((height > 2))
  celestia header get-by-height "$((height - 1))" --url http://127.0.0.1:26658 --token skip-auth |
    jq -e --argjson height "$((height - 1))" '.result.header.height | tonumber == $height' >/dev/null
else
  curl --max-time 2 -fsS http://127.0.0.1:26657/status | jq -e '.result.sync_info.latest_block_height | tonumber > 1' >/dev/null
  nc -z 127.0.0.1 7980
  curl --max-time 2 -fsS http://127.0.0.1:1317/valaddr/v1/all-bonded-fibre-providers |
    jq -e --arg host "${FIBRE_HOST:-localhost:7980}" \
      '.providers | length == 1 and .[0].info.host == $host' >/dev/null
fi
