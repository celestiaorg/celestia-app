package types

// NewDataCommitment creates a new DataCommitment
func NewDataCommitment(
	nonce uint64,
	beginBlock uint64,
	endBlock uint64,
) *DataCommitment {
	return &DataCommitment{
		Nonce:      nonce,
		BeginBlock: beginBlock,
		EndBlock:   endBlock,
	}
}

// DataCommitments is a collection of DataCommitment
type DataCommitments []DataCommitment

func (dc DataCommitments) Len() int {
	return len(dc)
}

func (dc DataCommitments) Less(i, j int) bool {
	return dc[i].Nonce > dc[j].Nonce
}

func (dc DataCommitments) Swap(i, j int) {
	dc[i], dc[j] = dc[j], dc[i]
}
