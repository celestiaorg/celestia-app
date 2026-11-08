//go:build pffcampaign

package sigcache

func (c *Cache) CampaignPurge() { c.entries.Purge() }
