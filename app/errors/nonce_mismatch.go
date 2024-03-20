package errors

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// IsNonceMismatch checks if the error is due to a sequence mismatch.
func IsNonceMismatch(err error) bool {
	return errors.Is(err, sdkerrors.ErrWrongSequence)
}

// IsNonceMismatch checks if the error code matches the sequence mismatch.
func IsNonceMismatchCode(code uint32) bool {
	return code == sdkerrors.ErrWrongSequence.ABCICode()
}

// ParseNonceMismatch extracts the expected sequence number from the
// ErrWrongSequence error.
func ParseNonceMismatch(err error) (uint64, error) {
	if !IsNonceMismatch(err) {
		return 0, errors.New("error is not a sequence mismatch")
	}

	return ParseExpectedSequence(err.Error())
}

// ParseExpectedSequence extracts the expected sequence number from the
// ErrWrongSequence error.
func ParseExpectedSequence(str string) (uint64, error) {
	if !strings.HasPrefix(str, "account sequence mismatch") {
		return 0, fmt.Errorf("unexpected wrong sequence error: %s", str)
	}

	numbers := regexpInt.FindAllString(str, -1)
	if len(numbers) != 2 {
		return 0, fmt.Errorf("expected two numbers in string, got %d", len(numbers))
	}

	// the first number is the expected sequence number
	return strconv.ParseUint(numbers[0], 10, 64)
}
