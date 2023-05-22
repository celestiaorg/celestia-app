package v1

const (
	// PruningThreshold is the number of blocks we will wait to prune a given
	// attestation. There is a single exception where we will not prune the last
	// valset update if there has not been one since.
	//
	// Ideally, we should not prune before the unbonding period has ended.
	// Roughly, assuming 15 second blocks, the unbonding period is 120960
	// blocks. To be abundantly careful, a significantly larger value was
	// chosen. In the worst case scenario, where state is pruned before the
	// unbonding period and orchestrators have not been signing for 2+ weeks, we
	// can manually query a snapshot or an archive node to get this state,
	// however it is extremely unlikely to be needed.
	PruningThreshold = 220000
)
