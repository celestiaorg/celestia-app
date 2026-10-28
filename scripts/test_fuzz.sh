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
# See docs/fuzzing.md for the risk-ranked entry-point inventory and limitations.
# =============================================================================

echo "Running fuzz tests..."
go test -run='^$' -fuzz=FuzzCheckTxPrepareProposal -fuzztime 5m ./app/test
go test -fuzz=FuzzPFBGasEstimation -fuzztime 5m ./x/blob/types
go test -fuzz=FuzzCountICAPacketMsgs -fuzztime 5m ./app
go test -fuzz=FuzzScatterMarshalParity -fuzztime 5m ./fibre/internal/grpc
go test -fuzz=FuzzShardCodecRoundTrip -fuzztime 5m ./fibre
go test -fuzz=FuzzShardCodecReadNoPanic -fuzztime 5m ./fibre
go test -fuzz=FuzzShardReader -fuzztime 5m ./fibre
go test -run='^$' -fuzz=FuzzProcessProposalMutatedTxs -fuzztime 5m ./app/test
go test -run='^$' -fuzz=FuzzPFFDecodeCandidateParity -fuzztime 5m -fuzzminimizetime=100x ./app
go test -run='^$' -fuzz=FuzzValidateBlobTx -fuzztime 5m -fuzzminimizetime=100x ./x/blob/types
go test -run='^$' -fuzz=FuzzMsgPayForBlobsValidateBasic -fuzztime 2m -fuzzminimizetime=100x ./x/blob/types
go test -run='^$' -fuzz=FuzzCheckTxMutatedTxs -fuzztime 5m ./app/test
# These targets are cheap per exec, so Go's default minimization budget would
# spend most of the run minimizing; cap it so the workers keep fuzzing.
go test -run='^$' -fuzz=FuzzParsePayForFibreTx -fuzztime 5m -fuzzminimizetime=100x ./x/fibre/types
go test -run='^$' -fuzz=FuzzShareProofValidate -fuzztime 5m -fuzzminimizetime=100x ./pkg/proof
go test -run='^$' -fuzz=FuzzDataAvailabilityHeaderFromProto -fuzztime 2m -fuzzminimizetime=100x ./pkg/da
go test -run='^$' -fuzz=FuzzNestedMsgDecorator -fuzztime 2m -fuzzminimizetime=100x ./app/ante
go test -run='^$' -fuzz=FuzzAnteFeeArithmetic -fuzztime 2m -fuzzminimizetime=100x ./app/ante
go test -run='^$' -fuzz=FuzzUploadViewsParity -fuzztime 5m -fuzzminimizetime=100x ./fibre/internal/grpc
go test -run='^$' -fuzz=FuzzDownloadViewsParity -fuzztime 5m -fuzzminimizetime=100x ./fibre/internal/grpc
go test -run='^$' -fuzz=FuzzRootFromProofs -fuzztime 2m -fuzzminimizetime=100x ./pkg/rsema1d/merkle
go test -run='^$' -fuzz=FuzzCompute -fuzztime 2m -fuzzminimizetime=100x ./pkg/rsema1d/rlc
go test -run='^$' -fuzz=FuzzSum -fuzztime 2m -fuzzminimizetime=100x ./pkg/rsema1d/internal/sha256x2
