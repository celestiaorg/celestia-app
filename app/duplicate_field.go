package app

import "google.golang.org/protobuf/encoding/protowire"

// txRawSignaturesField is the TxRaw field number for signatures. It is the only
// field ADR-027 permits to appear more than once.
const txRawSignaturesField = 3

// hasDuplicateTxRawField reports whether the raw transaction bytes contain a
// top-level protobuf field (other than signatures) that appears more than once.
//
// The SDK decoder reads a TxRaw as body_bytes(1), auth_info_bytes(2), and
// repeated signatures(3), resolving a duplicated scalar field to its last
// occurrence. go-square re-parses the same bytes with a schema that merges
// duplicated fields instead. A transaction that duplicates body_bytes or
// auth_info_bytes therefore classifies differently in the two components. This
// function detects such ambiguous encodings so they can be rejected before they
// are admitted to the mempool or included in a proposal.
//
// Undecodable bytes return false: the SDK decoder rejects those on its own.
func hasDuplicateTxRawField(txBytes []byte) bool {
	seen := make(map[protowire.Number]bool)
	for len(txBytes) > 0 {
		num, _, n := protowire.ConsumeField(txBytes)
		if n < 0 {
			return false
		}
		if num != txRawSignaturesField {
			if seen[num] {
				return true
			}
			seen[num] = true
		}
		txBytes = txBytes[n:]
	}
	return false
}
