package shares

// RoundUpPowerOfTwo returns the next power of two greater than or equal to input.
func RoundUpPowerOfTwo(input int) int {
	k := 1
	for k < input {
		k = k << 1
	}
	return k
}

// RoundDownPowerOfTwo returns the next power of two less than or equal to input.
func RoundDownPowerOfTwo(v int) int {
	c := RoundUpPowerOfTwo(v)
	if c == v {
		return c
	}
	return c / 2
}

// RoundUpPowerOfTwoU rounds input up to the next power of two.
// Examples:
//
//	RoundUpPowerOfTwoU(1) = 2
//	RoundUpPowerOfTwoU(2) = 4
//	RoundUpPowerOfTwoU(5) = 8
func RoundUpPowerOfTwoU(input uint64) uint64 {
	// keep track of the value to check if its the same later
	i := input

	// find the next highest power using bit mashing
	input--
	input |= input >> 1
	input |= input >> 2
	input |= input >> 4
	input |= input >> 8
	input |= input >> 16
	input |= input >> 32
	input++

	// force the value to the next highest power of two if its the same
	if input == i {
		return 2 * input
	}

	return input
}

// RoundDownPowerOfTwoU returns input if it is a power of two. If input isn't a
// power of two, it rounds input down to the next power of two that is lower
// than input. Examples:
//
//	RoundDownPowerOfTwoU(1) = 1
//	RoundDownPowerOfTwoU(2) = 2
//	RoundDownPowerOfTwoU(5) = 4
func RoundDownPowerOfTwoU(input uint64) uint64 {
	c := RoundUpPowerOfTwoU(input)
	if c == input {
		return c
	}
	return c / 2
}

// IsPowerOfTwoU returns true if input is a power of two.
func IsPowerOfTwoU(input uint64) bool {
	if input&(input-1) == 0 && input != 0 {
		return true
	}
	return false
}
