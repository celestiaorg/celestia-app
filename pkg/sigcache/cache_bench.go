//go:build benchmarks

package sigcache

// Purge removes every entry, so a benchmark can measure the cold path.
// Benchmarks only: a node never discards verifications it already paid for.
func (c *Cache) Purge() {
	c.entries.Purge()
}
