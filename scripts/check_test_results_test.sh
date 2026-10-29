#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
export CI_EVENT_NAME CI_TEST_RESULTS
jobs='["go-test", "test-util", "test-scripts", "test-e2e-compile", "test-multiplexer", "build-e2e-artifacts", "test-docker-e2e", "test-arm64"]'

expect_failure() {
    if bash "$SCRIPT_DIR/check_test_results.sh" > /dev/null 2>&1; then
        echo "Expected rejection: $CI_EVENT_NAME $CI_TEST_RESULTS" >&2
        exit 1
    fi
}

for CI_EVENT_NAME in pull_request merge_group push release workflow_dispatch; do
    case "$CI_EVENT_NAME" in
        pull_request) skipped='["test-util", "test-multiplexer", "build-e2e-artifacts", "test-docker-e2e"]' ;;
        merge_group) skipped='["go-test", "test-scripts", "test-e2e-compile"]' ;;
        *) skipped='["test-util", "test-scripts", "test-e2e-compile"]' ;;
    esac
    baseline=$(jq -n --argjson jobs "$jobs" --argjson skipped "$skipped" '
        reduce $jobs[] as $job ({};
            .[$job].result = (if $skipped | index($job) then "skipped" else "success" end))
    ')
    CI_TEST_RESULTS=$baseline bash "$SCRIPT_DIR/check_test_results.sh"

    # Every failure/cancellation must block, including in an intentionally
    # skipped job. Skipping or omitting any required job must also block.
    for job in $(jq -r '.[]' <<< "$jobs"); do
        for result in failure cancelled skipped; do
            if [[ "$result" == skipped ]] && jq -e --arg job "$job" 'index($job)' <<< "$skipped" > /dev/null; then
                continue
            fi
            CI_TEST_RESULTS=$(jq --arg job "$job" --arg result "$result" '.[$job].result = $result' <<< "$baseline")
            expect_failure
        done
        CI_TEST_RESULTS=$(jq --arg job "$job" 'del(.[$job])' <<< "$baseline")
        expect_failure
    done
done

CI_TEST_RESULTS='{}'
CI_EVENT_NAME=unexpected
expect_failure
CI_EVENT_NAME=merge_group
CI_TEST_RESULTS='not json'
expect_failure
echo "CI result gate regression checks passed"
