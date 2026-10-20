#!/usr/bin/env bash
set -euo pipefail

# Exercise the real coverage script in a tiny module without app dependencies.
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
FIXTURE_DIR=$(mktemp -d)
trap 'rm -rf "$FIXTURE_DIR"' EXIT
export GOWORK=off
cd "$FIXTURE_DIR"

cat > go.mod <<'EOF'
module example.com/coveragefixture

go 1.26.0
EOF
mkdir -p app library consumer second untested test/util tools/tool
cat > app/prepare_proposal.go <<'EOF'
package app
func PrepareProposalHandler() int { return 1 }
func Helper() int { return 2 }
EOF
cat > app/process_proposal.go <<'EOF'
package app
func ProcessProposalHandler() int { return 3 }
EOF
cat > library/library.go <<'EOF'
package library
func Value() int { return 42 }
func Unused() int { return 0 }
EOF
# Generated code and tools are out of scope even when tests execute them.
cat > library/library.pb.go <<'EOF'
package library
func Generated() int { return 1 }
EOF
cat > tools/tool/tool.go <<'EOF'
package tool
func Value() int { return 3 }
EOF
cat > tools/tool/tool_test.go <<'EOF'
package tool
import "testing"
func TestValue(t *testing.T) { _ = Value() }
EOF
cat > untested/untested.go <<'EOF'
package untested
func Value() int { return 7 }
EOF
cat > test/util/util.go <<'EOF'
package util
func Want() int { return 42 }
EOF
cat > consumer/consumer_test.go <<'EOF'
package consumer_test
import (
    "testing"
    "example.com/coveragefixture/app"
    "example.com/coveragefixture/library"
    "example.com/coveragefixture/test/util"
)
func TestValue(t *testing.T) {
    _ = app.PrepareProposalHandler() + app.ProcessProposalHandler() + app.Helper()
    if library.Value() != util.Want() { t.Fatal("wrong value") }
}
EOF
cp consumer/consumer_test.go second/consumer_test.go

bash "$SCRIPT_DIR/test_cover.sh"

# The library has no local tests: only its consumer can cover Value.
# Unexecuted statements and packages must remain in the denominator.
awk '
    NR == 1 { if ($0 != "mode: atomic") exit 1; next }
    seen[$1]++ { exit 1 }
    /\/test\/util\// { exit 1 }
    /\/tools\// || /\.pb\.go:/ { exit 1 }
    /\/library\/library.go:2\./ && $3 == 2 { covered = 1 }
    /\/library\/library.go:3\./ && $3 == 0 { uncovered = 1 }
    /\/untested\/untested.go:/ && $3 == 0 { untested = 1 }
    END { if (!covered || !uncovered || !untested) exit 1 }
' coverage.txt
go tool cover -func=coverage.txt > /dev/null
bash "$SCRIPT_DIR/check_proposal_coverage.sh"
# Helper execution must not mask an unexecuted or uninstrumented handler.
for handler in prepare process; do
    awk -v file="/app/${handler}_proposal.go:" '
        index($1, file) && $1 ~ /:2\./ { $3 = 0 }
        { print }
    ' coverage.txt > zero.txt
    if bash "$SCRIPT_DIR/check_proposal_coverage.sh" zero.txt; then
        echo "unexecuted $handler handler unexpectedly passed" >&2
        exit 1
    fi
    awk -v file="/app/${handler}_proposal.go:" '!index($1, file)' coverage.txt > missing.txt
    if bash "$SCRIPT_DIR/check_proposal_coverage.sh" missing.txt; then
        echo "missing $handler handler unexpectedly passed" >&2
        exit 1
    fi
done
if bash "$SCRIPT_DIR/check_proposal_coverage.sh" nonexistent.txt; then
    echo "missing coverage profile unexpectedly passed" >&2
    exit 1
fi

assert_no_temporary_profile() {
    local profiles=(coverage.??????)
    if [[ -e "${profiles[0]}" ]]; then
        echo "temporary coverage directory was not removed" >&2
        exit 1
    fi
}
assert_no_temporary_profile

# Shards run disjoint packages, and merging them must reproduce the full report.
mkdir -p app/test
cat > app/test/app_test.go <<'EOF'
package test
import (
    "testing"
    "example.com/coveragefixture/app"
)
func TestHandlers(t *testing.T) { _ = app.PrepareProposalHandler() + app.ProcessProposalHandler() }
EOF
bash "$SCRIPT_DIR/test_cover.sh"
mv coverage.txt full.txt
mkdir -p shards
COVER_SHARD=app-test bash "$SCRIPT_DIR/test_cover.sh" > shard.log 2>&1
mv coverage.txt shards/app-test.txt
# fibre/... matches nothing here, so this shard runs only tools/tool.
COVER_SHARD=fibre-tools bash "$SCRIPT_DIR/test_cover.sh" > shard.log 2>&1
mv coverage.txt shards/fibre-tools.txt
COVER_SHARD=rest bash "$SCRIPT_DIR/test_cover.sh" > shard.log 2>&1
# -coverpkg lists every scoped package in each result line, so match the tested package.
grep -qE '^ok[[:space:]]+example.com/coveragefixture/consumer[[:space:]]' shard.log
if grep -qE '^ok[[:space:]]+example.com/coveragefixture/(app/test|tools/tool)[[:space:]]' shard.log; then
    echo "rest shard ran a package owned by another shard" >&2
    exit 1
fi
mv coverage.txt shards/rest.txt
bash "$SCRIPT_DIR/merge_coverage.sh" coverage.txt shards/app-test.txt shards/fibre-tools.txt shards/rest.txt
cmp full.txt coverage.txt
if cmp -s full.txt shards/rest.txt; then
    echo "app-test shard did not contribute coverage" >&2
    exit 1
fi
if COVER_SHARD=unknown bash "$SCRIPT_DIR/test_cover.sh" > shard.log 2>&1; then
    echo "unknown coverage shard unexpectedly passed" >&2
    exit 1
fi
bash "$SCRIPT_DIR/test_cover.sh" --list-shards | grep -qx rest
assert_no_temporary_profile
rm -rf app/test shards full.txt

# A failed run must neither replace a previous report nor publish a partial one.
cp coverage.txt previous.txt
cat > consumer/failure_test.go <<'EOF'
package consumer_test
import "testing"
func TestFailure(t *testing.T) { t.Fatal("intentional failure") }
EOF
if bash "$SCRIPT_DIR/test_cover.sh" > failure.log 2>&1; then
    echo "coverage script unexpectedly succeeded" >&2
    exit 1
fi
cmp previous.txt coverage.txt
assert_no_temporary_profile
rm coverage.txt
if bash "$SCRIPT_DIR/test_cover.sh" > failure.log 2>&1; then
    echo "coverage script unexpectedly succeeded" >&2
    exit 1
fi
[[ ! -e coverage.txt ]]
assert_no_temporary_profile
echo "coverage script regression checks passed"
