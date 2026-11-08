//go:build pffcampaign

package app

// CampaignPurge clears only application-owned caches, after all work has joined.
func (app *App) CampaignPurge() {
	app.sigCache.CampaignPurge()
	app.txCache.entries.Purge()
	app.CampaignPurgeProposal()
	app.FibreKeeper.CampaignPurgeValidators()
	app.campaignPurgeRecheck()
}
func (app *App) CampaignPurgeProposal() {
	app.proposalCache.mu.Lock()
	defer app.proposalCache.mu.Unlock()
	app.proposalCache.entry = nil
}
func (app *App) CampaignCacheStats() map[string]int {
	stats := map[string]int{"signature_entries": app.sigCache.Len(), "signature_capacity": SigCacheCapacity(5000), "blob_capacity": defaultTxCacheCapacity, "proposal_capacity": 1, "validator_set_capacity": 512}
	app.campaignRecheckStats(stats)
	return stats
}
