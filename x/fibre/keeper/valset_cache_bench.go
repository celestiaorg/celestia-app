//go:build benchmarks

package keeper

// PurgeValidatorSetCache drops every memoized validator set conversion, so a
// benchmark can measure a node that has not seen these heights before.
//
// Benchmarks only.
func (k Keeper) PurgeValidatorSetCache() {
	if k.valSetCache != nil {
		k.valSetCache.Purge()
	}
}
