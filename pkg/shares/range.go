package shares

// Range is an end exclusive set of share indexes.
type Range struct {
	// Start is the index of the first share occupied by this range.
	Start int
	// End is the next index after the last share occupied by this range.
	End int
}

func NewRange(start, end int) Range {
	return Range{Start: start, End: end}
}

func EmptyRange() Range {
	return Range{Start: 0, End: 0}
}

func (r Range) IsEmpty() bool {
	return r.Start == 0 && r.End == 0
}

func (r *Range) Add(value int) {
	r.Start += value
	r.End += value
}
