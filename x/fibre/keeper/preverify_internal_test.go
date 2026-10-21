package keeper

import (
	"sync/atomic"
	"testing"

	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/stretchr/testify/require"
)

func TestVerificationChunkSize(t *testing.T) {
	tests := map[string]struct {
		items, procs, want int
	}{
		"CheckTx's promise and certificate": {items: 2, procs: 16, want: 1},
		"one item per proc":                 {items: 16, procs: 16, want: 1},
		"a few items per proc":              {items: 40, procs: 16, want: 3},
		"a 100-PFF block":                   {items: 200, procs: 16, want: maxChunkSize},
		"a full block":                      {items: 5800, procs: 16, want: maxChunkSize},
		"one proc, a few items":             {items: 2, procs: 1, want: 2},
		"one proc, many items":              {items: 15, procs: 1, want: maxChunkSize},
		"no items":                          {items: 0, procs: 16, want: 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, verificationChunkSize(tc.items, tc.procs))
		})
	}
}

// TestRunVerificationsSurvivesWorkerPanics pins what ProcessProposal relies on: a
// panicking worker dies alone, and its unfinished items stay unverified for the walk.
func TestRunVerificationsSurvivesWorkerPanics(t *testing.T) {
	message := []byte("sign bytes")
	items := make([]verification, 64)
	valid := make([]bool, len(items))
	for i := range items {
		priv := ed25519.GenPrivKey()
		signature, err := priv.Sign(message)
		require.NoError(t, err)
		if valid[i] = i%5 != 0; !valid[i] {
			signature[0] ^= 0x01
		}
		items[i] = verification{message: message, pubKeys: []crypto.PubKey{priv.PubKey()}, signatures: [][]byte{signature}}
	}

	for _, stopOnFirstFailure := range []bool{false, true} {
		var published atomic.Int32
		var verified []bool
		require.NotPanics(t, func() {
			verified = runVerifications(items, stopOnFirstFailure, func([]atomic.Bool, int, int) {
				published.Add(1)
				panic("publish panic")
			})
		})
		require.Positive(t, published.Load(), "no worker panicked")
		for i, ok := range verified {
			require.False(t, ok && !valid[i], "item %d: invalid signature reported verified", i)
		}
	}
}
