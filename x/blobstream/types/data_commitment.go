package types

import "time"

var _ AttestationRequestI = &DataCommitment{}

// NewDataCommitment creates a new DataCommitment.
func NewDataCommitment(
	nonce uint64,
	beginBlock uint64,
	endBlock uint64,
	blockTime time.Time,
) *DataCommitment {
	return &DataCommitment{
		Nonce:      nonce,
		BeginBlock: beginBlock,
		EndBlock:   endBlock,
		Time:       blockTime,
	}
}

func (m *DataCommitment) BlockTime() time.Time {
	return m.Time
}
