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
func RoundDownPowerOfTwo[I constraints.Integer](input I) (I, error) {
	if input <= 0 {
		return 0, fmt.Errorf("input %v must be positive", input)
	}
	roundedUp := RoundUpPowerOfTwo(input)
	if roundedUp == input {
		return roundedUp, nil
	}
	return roundedUp / 2, nil
}

// RoundUpPowerOfTwo returns the next power of two that is strictly greater than input.
func RoundUpPowerOfTwoStrict[I constraints.Integer](input I) I {
	result := RoundUpPowerOfTwo(input)

	// round the result up to the next power of two if is equal to the input
	if result == input {
		return result * 2
	}
	return result
}

// IsPowerOfTwo returns true if input is a power of two.
func IsPowerOfTwo[I constraints.Integer](input I) bool {
	return input&(input-1) == 0 && input != 0
}
