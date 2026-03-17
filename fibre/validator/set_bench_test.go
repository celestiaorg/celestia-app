package validator_test

import (
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
)

// BenchmarkSet_Assign measures row assignment performance.
// Dominated by Fisher-Yates shuffle of 16384 rows, constant across validator counts.
//
// Results (AMD Ryzen 9 7940HS):
//
//	10_validators     135 µs/op   235 kB/op    18 allocs/op
//	50_validators     136 µs/op   239 kB/op    62 allocs/op
//	100_validators    135 µs/op   244 kB/op   114 allocs/op
func BenchmarkSet_Assign(b *testing.B) {
	for _, n := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("%d_validators", n), func(b *testing.B) {
			valSet := makeBenchValidatorSet(n)
			for b.Loop() {
				_ = valSet.Assign(testCommitment, 16384, 4096, 16, testLivenessThreshold)
			}
		})
	}
}

// BenchmarkSet_Select measures validator selection performance.
// Scales O(n²) due to stake-weighted shuffle but remains negligible for typical validator counts.
//
// Results (AMD Ryzen 9 7940HS):
//
//	10_validators     196 ns/op    96 B/op     2 allocs/op
//	50_validators     1.5 µs/op   432 B/op     2 allocs/op
//	100_validators    3.7 µs/op   912 B/op     2 allocs/op
func BenchmarkSet_Select(b *testing.B) {
	for _, n := range []int{10, 50, 100} {
		b.Run(fmt.Sprintf("%d_validators", n), func(b *testing.B) {
			valSet := makeBenchValidatorSet(n)
			for b.Loop() {
				_ = valSet.Select(testOriginalRows, testMinRows, testLivenessThreshold)
			}
		})
	}
}

func makeBenchValidatorSet(n int) validator.Set {
	validators := make([]*core.Validator, n)
	for i := range n {
		validators[i] = core.NewValidator(ed25519.GenPrivKey().PubKey(), 1)
	}
	return validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}
}
