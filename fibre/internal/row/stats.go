package row

// PoolStats reports retained capacity, not resident physical memory. Mapped
// bytes exclude slabs already handed to the asynchronous unmap queue.
type PoolStats struct {
	RowCount             int    `json:"row_count"`
	InUseSlabs           int    `json:"in_use_slabs"`
	FreeSlabs            int    `json:"free_slabs"`
	InUseBytes           int64  `json:"in_use_bytes"`
	FreeBytes            int64  `json:"free_bytes"`
	MappedBytes          int64  `json:"mapped_bytes"`
	Requests             uint64 `json:"requests"`
	Allocations          uint64 `json:"allocations"`
	LastRequestedRowSize int64  `json:"last_requested_row_size"`
	MaxRequestedRowSize  int64  `json:"max_requested_row_size"`
}

// Stats samples buckets independently, briefly holding each bucket's lock.
func (p *Pool) Stats() PoolStats {
	if p == nil {
		return PoolStats{}
	}
	s := PoolStats{RowCount: p.rowCount, Requests: p.requests.Load(), Allocations: p.allocations.Load(), LastRequestedRowSize: p.lastRequestedRowSize.Load(), MaxRequestedRowSize: p.maxRequestedRowSize.Load()}
	for i := range p.buckets {
		bk := &p.buckets[i]
		bk.Lock()
		s.InUseSlabs += len(bk.used)
		s.FreeSlabs += len(bk.free)
		for _, slab := range bk.used {
			s.InUseBytes += int64(len(slab.region))
			if slab.mapped {
				s.MappedBytes += int64(len(slab.region))
			}
		}
		for _, slab := range bk.free {
			s.FreeBytes += int64(len(slab.region))
			if slab.mapped {
				s.MappedBytes += int64(len(slab.region))
			}
		}
		bk.Unlock()
	}
	return s
}

// MemoryStats reports pooled parity/partial rows and Merkle tree capacity.
func (a *Assembler) MemoryStats() (PoolStats, PoolStats) {
	if a == nil {
		return PoolStats{}, PoolStats{}
	}
	return a.rowsPool.Stats(), a.treePool.Stats()
}
