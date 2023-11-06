#!/usr/bin/env bash
set -e

echo "Running fuzz tests..."
# manually specify the tests to fuzz since go toolchain doesn't support
# fuzzing multiple packages with multiple fuzz tests
go test -fuzz=FuzzNewInfoByte -fuzztime 1m ./pkg/shares
go test -fuzz=FuzzValidSequenceLen -fuzztime 1m ./pkg/shares
go test -fuzz=FuzzSquare -fuzztime 5m ./pkg/square
go test -fuzz=FuzzPFBGasEstimation -fuzztime 3m ./x/blob/types
