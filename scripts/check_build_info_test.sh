#!/usr/bin/env bash
set -euo pipefail

# Exercise scripts/check-build-info.sh on tiny binaries built with and without
# the settings that the release binaries use.
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CHECK="$SCRIPT_DIR/check-build-info.sh"
FIXTURE_DIR=$(mktemp -d)
trap 'rm -rf "$FIXTURE_DIR"' EXIT
export GOWORK=off GOTOOLCHAIN=local GOFLAGS=
cd "$FIXTURE_DIR"

local_go=$(go env GOVERSION)
printf 'module example.com/buildinfofixture\n\ngo %s\n' "${local_go#go}" > go.mod
printf 'package main\n\nfunc main() {}\n' > main.go
printf 'module example.com/buildinfofixture\n\ngo 1.21.0\n' > other.go.mod

go build -trimpath -o trimmed .
go build -o untrimmed .

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

"$CHECK" trimmed go.mod >/dev/null || fail "a -trimpath binary built with the go.mod version must pass"
if "$CHECK" untrimmed go.mod >/dev/null; then
  fail "a binary built without -trimpath must fail"
fi
if "$CHECK" trimmed other.go.mod >/dev/null; then
  fail "a binary built with another Go version than go.mod must fail"
fi

echo "PASS: check-build-info.sh"
