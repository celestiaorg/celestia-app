package appconsts

import "time"

const (
	// MaxAgeDuration is the maximum age of evidence that can be submitted for
	// slashing. See CIP-037.
	MaxAgeDuration = 337 * time.Hour // (14 days + 1 hour)

	// MaxAgeNumBlocks is the maximum number of blocks for which evidence can be
	// submitted for slashing. Evidence is stale only once both MaxAgeDuration
	// and MaxAgeNumBlocks are exceeded, so the block bound must not admit
	// evidence for longer than MaxAgeDuration (the unbonding period); otherwise
	// stake that should absorb the slash can mature and exit first. This value
	// is UnbondingTime divided by a conservative 3s upper bound on block time
	// (337h / 3s = 404,400), which keeps the evidence window within the
	// unbonding period at block times up to 3s.
	MaxAgeNumBlocks = 404_400
)
