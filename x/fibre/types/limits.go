package types

// Decode-time cardinality bounds for a BlobShard. The fibre-proto codec enforces
// them before the protobuf decoder allocates per-row and per-proof objects, so a
// peer cannot amplify a compact message (an empty BlobRow is ~2 wire bytes but a
// live heap object) into large decoded memory.
//
// The values are the protocol v0 maxima. TestDecodeLimitsMatchParams in the fibre
// package asserts they stay in sync with ProtocolParams.
const (
	// MaxBlobRowsPerShard bounds BlobShard.Rows. A shard holds only the rows
	// assigned to a single validator, which never exceeds MaxRowsPerValidator
	// (4096 for the default v0 parameters).
	MaxBlobRowsPerShard = 4096

	// MaxProofSegmentsPerRow bounds BlobRow.Proof. A row's Merkle inclusion proof
	// has ceil(log2(TotalRows)) segments (14 for the default v0 parameters, where
	// TotalRows is 16384).
	MaxProofSegmentsPerRow = 14
)
