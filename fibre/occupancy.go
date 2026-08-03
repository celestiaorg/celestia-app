package fibre

import "sync/atomic"

// occupancy tracks how many bytes of shards a validator holds against its disk
// budget.
type occupancy struct {
	used atomic.Int64 // on-disk plus in-flight bytes
	// budget is the per-node cap in bytes. A non-positive value (<=0) disables
	// the limiter.
	budget atomic.Int64
}

// newOccupancy returns a counter with the given per-node budget in bytes.
// A non-positive budget disables the limit.
func newOccupancy(budget int64) *occupancy {
	occ := &occupancy{}
	occ.budget.Store(budget)
	return occ
}

// setBudget updates the per-node budget.
// Can be recomputed when the validator's stake or the governance parameter changes.
func (o *occupancy) setBudget(newBudget int64) {
	o.budget.Store(newBudget)
}

// usage returns the current tracked occupancy in bytes (on-disk plus in-flight).
func (o *occupancy) usage() int64 {
	return o.used.Load()
}

// budgetBytes returns the current per-node budget in bytes (<= 0 means no limit).
func (o *occupancy) budgetBytes() int64 {
	return o.budget.Load()
}

// seed sets the initial store usage. Should be called at
// startup before the server accepts uploads.
func (o *occupancy) seed(n int64) {
	o.used.Store(n)
}

// release drops n bytes from usage after a store failure or a prune removed them.
func (o *occupancy) release(n int64) {
	o.used.Add(-n)
}

// reserve admits n bytes if they fit under the budget and counts them, returning
// false without reserving when the upload would exceed it. A non-positive budget
// means no limit.
func (o *occupancy) reserve(n int64) bool {
	budget := o.budget.Load()
	if budget <= 0 {
		o.used.Add(n) // no limit set
		return true
	}

	if o.used.Add(n) <= budget {
		return true
	}
	// release added bytes if does not fit in current budget
	o.used.Add(-n)
	return false
}
