#!/usr/bin/env bash
set -euo pipefail

# Only the jobs deliberately omitted at this stage may be skipped. Coverage is
# enforced separately by the required test-coverage workflow on merge groups.
case "${CI_EVENT_NAME:?CI_EVENT_NAME is required}" in
    pull_request)
        required='["go-test", "test-scripts", "test-e2e-compile", "test-arm64"]'
        skipped='["test-util", "test-multiplexer", "build-e2e-artifacts", "test-docker-e2e"]'
        ;;
    merge_group)
        required='["test-util", "test-multiplexer", "build-e2e-artifacts", "test-docker-e2e", "test-arm64"]'
        skipped='["go-test", "test-scripts", "test-e2e-compile"]'
        ;;
    push|release|workflow_dispatch)
        required='["go-test", "test-multiplexer", "build-e2e-artifacts", "test-docker-e2e", "test-arm64"]'
        skipped='["test-util", "test-scripts", "test-e2e-compile"]'
        ;;
    *)
        echo "Unexpected CI event: $CI_EVENT_NAME" >&2
        exit 1
        ;;
esac

if ! jq -e --argjson required "$required" --argjson skipped "$skipped" '
    . as $results |
    (keys == (($required + $skipped) | sort)) and
    all($required[]; $results[.].result == "success") and
    all($skipped[]; $results[.].result == "skipped")
' <<< "${CI_TEST_RESULTS:?CI_TEST_RESULTS is required}" > /dev/null; then
    echo "Test jobs failed, were cancelled, are missing, or were unexpectedly skipped" >&2
    exit 1
fi
echo "All tests passed for $CI_EVENT_NAME"
