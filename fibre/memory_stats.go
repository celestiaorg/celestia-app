package fibre

import "github.com/celestiaorg/celestia-app/v10/fibre/internal/row"

// PoolMemoryStats describes allocated capacity; it does not measure RSS.
type PoolMemoryStats = row.PoolStats

// BlobMemoryStats separates reusable row storage from codec scratch memory.
type BlobMemoryStats struct {
	Parity    PoolMemoryStats `json:"parity"`
	Trees     PoolMemoryStats `json:"trees"`
	Scratch   PoolMemoryStats `json:"scratch"`
	Downloads PoolMemoryStats `json:"downloads"`
}

// MemoryStats samples the pools shared by this blob configuration.
func (c BlobConfig) MemoryStats() BlobMemoryStats {
	parity, trees := c.Assembler.MemoryStats()
	return BlobMemoryStats{Parity: parity, Trees: trees, Scratch: c.workPool.Stats(), Downloads: c.DataPool.Stats()}
}
