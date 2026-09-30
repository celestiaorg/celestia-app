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
mkdir -p library consumer second untested test/util
cat > library/library.go <<'EOF'
package library
func Value() int { return 42 }
func Unused() int { return 0 }
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
    "example.com/coveragefixture/library"
    "example.com/coveragefixture/test/util"
)
func TestValue(t *testing.T) {
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
    /\/library\/library.go:2\./ && $3 == 2 { covered = 1 }
    /\/library\/library.go:3\./ && $3 == 0 { uncovered = 1 }
    /\/untested\/untested.go:/ && $3 == 0 { untested = 1 }
    END { if (!covered || !uncovered || !untested) exit 1 }
' coverage.txt
go tool cover -func=coverage.txt > /dev/null

assert_no_temporary_profile() {
    local profiles=(coverage.??????)
    if [[ -e "${profiles[0]}" ]]; then
        echo "temporary coverage directory was not removed" >&2
        exit 1
    fi
}
assert_no_temporary_profile

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
