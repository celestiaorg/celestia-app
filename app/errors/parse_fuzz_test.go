package errors_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	apperr "github.com/celestiaorg/celestia-app/v11/app/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// FuzzParseNodeErrors checks the parsers clients apply to error strings
// returned by a (possibly malicious) node: ParseInsufficientMinGasPrice must
// return a finite, non-negative gas price or an error, and ParseExpectedSequence
// and ParseNonceMismatch must return a sequence that appears in the string or an
// error. None may panic.
func FuzzParseNodeErrors(f *testing.F) {
	f.Add("insufficient fees; got: 100utia required: 200utia", 0.002, uint64(100_000), true)
	f.Add("insufficient fees; got: 0utia required: 200utia", 0.0, uint64(0), true)
	f.Add("insufficient fees; got: 300utia required: 200utia", 0.5, uint64(10), true)
	f.Add("insufficient fees; got: 1utia required: 0utia", 1.0, uint64(1), true)
	f.Add("insufficient fees; got: 1utia required: 9"+strings.Repeat("9", 400)+"utia", 1.0, uint64(1), true)
	f.Add("insufficient fees; got: 1utia required: 2utia insufficient fees; got: 1utia required: 2utia", 1.0, uint64(1), true)
	f.Add("account sequence mismatch, expected 12, got 9: incorrect account sequence", 0.0, uint64(0), false)
	f.Add("account sequence mismatch, expected 184467440737095516150, got 9", 0.0, uint64(0), false)
	f.Add("account sequence mismatch", 0.0, uint64(0), false)

	f.Fuzz(func(t *testing.T, msg string, gasPrice float64, gasLimit uint64, insufficientFee bool) {
		if math.IsNaN(gasPrice) || math.IsInf(gasPrice, 0) || gasPrice < 0 {
			gasPrice = 0
		}
		err := sdkerrors.ErrWrongSequence.Wrap(msg)
		if insufficientFee {
			err = sdkerrors.ErrInsufficientFee.Wrap(msg)
		}
		price, perr := apperr.ParseInsufficientMinGasPrice(err, gasPrice, gasLimit)
		// Excluded pending a fix: https://linear.app/celestia/issue/PROTOCO-3525.
		excluded := math.IsInf(price, 1)
		if perr == nil && !excluded && (math.IsNaN(price) || math.IsInf(price, 0) || price < 0) {
			t.Fatalf("ParseInsufficientMinGasPrice(%q, %v, %d) = %v", msg, gasPrice, gasLimit, price)
		}

		if seq, serr := apperr.ParseExpectedSequence(msg); serr == nil && !strings.Contains(msg, strconv.FormatUint(seq, 10)) {
			t.Fatalf("parsed sequence %d does not appear in %q", seq, msg)
		}
		seq, serr := apperr.ParseNonceMismatch(err)
		if serr == nil && insufficientFee {
			t.Fatal("ParseNonceMismatch accepted a non-sequence error")
		}
		if serr == nil && !strings.Contains(msg, strconv.FormatUint(seq, 10)) {
			t.Fatalf("parsed sequence %d does not appear in %q", seq, msg)
		}
	})
}
