#!/usr/bin/env bash
set -e

# =============================================================================
# WARNING: These fuzzers are a defense line against wire-format / codec bugs.
# The fibre scatter codec (fibre/internal/grpc/codec_scatter.go) is a hand-rolled
# marshaler that MUST stay byte-for-byte identical to gogoproto's canonical
# output, and the fibre shard codec is the on-the-wire shard encoding. DO NOT
# remove or disable any target below without an equivalent replacement; silently
# dropping one re-opens https://github.com/celestiaorg/celestia-app/issues/7392.
#
# The Go toolchain only fuzzes one target in one package per invocation, so each
# target is listed explicitly. When you add codec/encoding logic, add its fuzz
# target here too. If you rename a fuzz function, update the matching line below.
# =============================================================================

# run_fuzz retries once on failure. The Go fuzzing engine can spuriously fail
# a target with "context deadline exceeded" right at the -fuzztime boundary
# even when no crasher was found; this is an unfixed upstream toolchain flake
# (https://github.com/golang/go/issues/72088), not a bug in the target. A real
# crasher is written to testdata/fuzz and reproduces deterministically, so it
# still fails the retry.
run_fuzz() {
  if ! go test -fuzz="$1" -fuzztime 5m "$2"; then
    echo "$1 failed, retrying once (known go test -fuzz flake, see script comment)..."
    go test -fuzz="$1" -fuzztime 5m "$2"
  fi
}

echo "Running fuzz tests..."
run_fuzz FuzzPFBGasEstimation ./x/blob/types
run_fuzz FuzzScatterMarshalParity ./fibre/internal/grpc
run_fuzz FuzzShardCodecRoundTrip ./fibre
run_fuzz FuzzShardCodecReadNoPanic ./fibre
