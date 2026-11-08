//go:build pffcampaign

package keeper

func (k Keeper) CampaignPurgeValidators() {
	if k.valSetCache != nil {
		k.valSetCache.Purge()
	}
}
