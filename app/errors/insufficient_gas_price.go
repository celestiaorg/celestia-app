package errors

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	// This is relatively brittle. It would be better if going below the min gas price
	// had a specific error type.
	regexpMinGasPrice = regexp.MustCompile(`insufficient fees; got: \d+utia required: \d+utia`)
	regexpInt         = regexp.MustCompile(`[0-9]+`)
)

// ParseInsufficientMinGasPrice checks if the error is due to the gas price being too low.
// Given the previous gas price and gas limit, it returns the new minimum gas price that
// the node should accept.
// If the error is not due to the gas price being too low, it returns 0, nil
func ParseInsufficientMinGasPrice(err error, gasPrice float64, gasLimit uint64) (float64, error) {
	// first check if the error is ErrInsufficientFee
	if err == nil || !sdkerrors.ErrInsufficientFee.Is(err) {
		return 0, nil
	}

	// As there are multiple cases of ErrInsufficientFunds, we need to check the error message
	// matches the regexp
	substr := regexpMinGasPrice.FindAllString(err.Error(), -1)
	if len(substr) != 1 {
		return 0, nil
	}

	// extract the first and second numbers from the error message (got and required)
	numbers := regexpInt.FindAllString(substr[0], -1)
	if len(numbers) != 2 {
		return 0, fmt.Errorf("expected two numbers in error message got %d", len(numbers))
	}

	// attempt to parse them into float64 values
	got, err := strconv.ParseFloat(numbers[0], 64)
	if err != nil {
		return 0, err
	}
	required, err := strconv.ParseFloat(numbers[1], 64)
	if err != nil {
		return 0, err
	}

	// catch rare condition that required is zero. This should theoretically
	// never happen as a min gas price of zero should always be accepted.
	if required == 0 {
		return 0, errors.New("unexpected case: required gas price is zero (why was an error returned)")
	}

	// calculate the actual min gas price of the node based on the difference
	// between the got and required values. If gas price was zero, we need to use
	// the gasLimit to infer this.
	if gasPrice == 0 || got == 0 {
		if gasLimit == 0 {
			return 0, fmt.Errorf("gas limit and gas price cannot be zero")
		}
		return required / float64(gasLimit), nil
	}
	return required / got * gasPrice, nil
}

// IsInsufficientMinGasPrice checks if the error is due to the gas price being too low.
func IsInsufficientMinGasPrice(err error) bool {
	// first check if the error is ErrInsufficientFee
	if err == nil || !sdkerrors.ErrInsufficientFee.Is(err) {
		return false
	}

	// As there are multiple cases of ErrInsufficientFunds, we need to check the error message
	// matches the regexp
	return regexpMinGasPrice.MatchString(err.Error())
}
