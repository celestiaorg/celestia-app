package errors

import (
	"errors"
	"fmt"
	"strconv"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// IsNonceMismatch checks if the error is due to a sequence mismatch.
func IsNonceMismatch(err error) bool {
	return errors.Is(err, sdkerrors.ErrWrongSequence)
}

// ParseNonceMismatch extracts the expected sequence number from the
// ErrWrongSequence error.
func ParseNonceMismatch(err error) (uint64, error) {
	if !IsNonceMismatch(err) {
		return 0, errors.New("error is not a sequence mismatch")
	}

	numbers := regexpInt.FindAllString(err.Error(), -1)
	if len(numbers) != 2 {
		return 0, fmt.Errorf("unexpected wrong sequence error: %w", err)
	}

	// the first number is the expected sequence number
	return strconv.ParseUint(numbers[0], 10, 64)
}
