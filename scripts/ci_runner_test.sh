#!/usr/bin/env bash
set -euo pipefail

# Jobs in the workflows that run in the merge queue must pick their runner from
# the TRUSTED_CI_RUNNER repository variable, so the merge queue can move to a
# paid runner pool without a code change. Fails on any job that pins
# ubuntu-latest directly.
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
workflows=(build.yml lint.yml test.yml test-coverage.yml protobuf.yml)
expected="runs-on: \${{ github.event_name != 'pull_request' && vars.TRUSTED_CI_RUNNER || 'ubuntu-latest' }}"

status=0
for wf in "${workflows[@]}"; do
    file="$ROOT/.github/workflows/$wf"
    while IFS= read -r line; do
        trimmed="${line#"${line%%[![:space:]]*}"}"
        if [[ "$trimmed" == runs-on:* && "$trimmed" == *ubuntu-latest* && "$trimmed" != "$expected" ]]; then
            echo "$wf: job pins a runner instead of using TRUSTED_CI_RUNNER: $trimmed" >&2
            status=1
        fi
    done < "$file"
done
if [[ "$status" -ne 0 ]]; then
    exit 1
fi
echo "CI runner checks passed"
