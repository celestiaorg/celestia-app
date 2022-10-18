package shares

import (
	"fmt"

	"golang.org/x/exp/constraints"
)

// RoundUpPowerOfTwo returns the next power of two greater than or equal to input.
func RoundUpPowerOfTwo[I constraints.Integer](input I) I {
	var result I = 1
	for result < input {
		result = result << 1
	}
	return result
}

// RoundDownPowerOfTwo returns the next power of two less than or equal to input.
func RoundDownPowerOfTwo[I constraints.Integer](input I) I {
	if input <= 0 {
		panic(fmt.Sprintf("input %v must be positive", input))
	}
	roundedUp := RoundUpPowerOfTwo(input)
	if roundedUp == input {
		return roundedUp
	}
	return roundedUp / 2
}

// RoundUpPowerOfTwo returns the next power of two that is strictly greater than input.
func RoundUpPowerOfTwoStrict[I constraints.Integer](input I) I {
	var result I = 1
	for result < input {
		result = result << 1
	}

	// force the result to the next highest power of two if its the same as the input
	if result == input {
		return 2 * result
	}

	return result
}

// IsPowerOfTwo returns true if input is a power of two.
func IsPowerOfTwo[I constraints.Integer](input I) bool {
	return input&(input-1) == 0 && input != 0
}
