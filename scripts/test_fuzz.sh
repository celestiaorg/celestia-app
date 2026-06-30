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

echo "Running fuzz tests..."
go test -fuzz=FuzzPFBGasEstimation -fuzztime 5m ./x/blob/types
go test -fuzz=FuzzScatterMarshalParity -fuzztime 5m ./fibre/internal/grpc
go test -fuzz=FuzzShardCodecRoundTrip -fuzztime 5m ./fibre
go test -fuzz=FuzzShardCodecReadNoPanic -fuzztime 5m ./fibre
