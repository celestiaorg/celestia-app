package util

// NextHighestPowerOf2 returns the next lowest power of 2 unless the input is a power
// of two, in which case it returns the input
func NextHighestPowerOf2(v uint64) uint64 {
	// keep track of the value to check if its the same later
	i := v

	// find the next highest power using bit mashing
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v |= v >> 32
	v++

	// force the value to the next highest power of two if its the same
	if v == i {
		return 2 * v
	}

	return v
}

// NextLowestPowerOf2 calculates the next lowest power of 2 unless the input is
// a power of two, in which case it returns the input
func NextLowestPowerOf2(v uint64) uint64 {
	c := NextHighestPowerOf2(v)
	if c == v {
		return c
	}
	return c / 2
}

// IsPowerOf2 checks if number is power of 2
func IsPowerOf2(v uint64) bool {
	if v&(v-1) == 0 && v != 0 {
		return true
	} else {
		return false
	}
}
