package keeper

import (
	"bytes"
	"sync/atomic"
	"testing"

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

// newCertificateItem returns a work item of size signatures over message, all
// of them valid, along with the keys that signed it.
func newCertificateItem(t testing.TB, message []byte, size int) (verification, []ed25519.PrivKey) {
	t.Helper()

	item := verification{message: message}
	privKeys := make([]ed25519.PrivKey, size)
	for i := range privKeys {
		privKeys[i] = ed25519.GenPrivKey()
		signature, err := privKeys[i].Sign(message)
		require.NoError(t, err)
		item.pubKeys = append(item.pubKeys, privKeys[i].PubKey())
		item.signatures = append(item.signatures, signature)
	}
	return item, privKeys
}

// sequentialVerdict is the verdict the sequential verifier reaches: every
// signature checked on its own.
func sequentialVerdict(item verification) bool {
	for i, pubKey := range item.pubKeys {
		if !pubKey.VerifySignature(item.message, item.signatures[i]) {
			return false
		}
	}
	return true
}

// TestVerifyBatchOnlyMatchesTheSequentialVerdict checks that deciding on the
// batch equation alone reports the sequential verifier's verdict, through both
// the single-item and the chunked path.
func TestVerifyBatchOnlyMatchesTheSequentialVerdict(t *testing.T) {
	message := []byte("payment promise sign bytes")

	// Forged, not malformed: random bytes are rejected before any curve
	// arithmetic, so they would not exercise the equation at all.
	forge := func(t testing.TB, item *verification, privKeys []ed25519.PrivKey, index int) {
		t.Helper()

		signature, err := privKeys[index].Sign([]byte("sign bytes of another promise"))
		require.NoError(t, err)
		item.signatures[index] = signature
	}

	tests := map[string]func(t testing.TB, item *verification, privKeys []ed25519.PrivKey){
		"all signatures valid": func(testing.TB, *verification, []ed25519.PrivKey) {},
		"forged at the first index": func(t testing.TB, item *verification, privKeys []ed25519.PrivKey) {
			forge(t, item, privKeys, 0)
		},
		"forged at the last index": func(t testing.TB, item *verification, privKeys []ed25519.PrivKey) {
			forge(t, item, privKeys, batchSize-1)
		},
		"non-canonical signature": func(_ testing.TB, item *verification, _ []ed25519.PrivKey) {
			item.signatures[7] = bytes.Repeat([]byte{0xff}, ed25519.SignatureSize)
		},
	}

	batch := ed25519.NewBatchVerifier().(*ed25519.BatchVerifier)
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			for i := range 20 {
				valid, _ := newCertificateItem(t, message, batchSize)
				item, privKeys := newCertificateItem(t, message, batchSize)
				mutate(t, &item, privKeys)
				want := sequentialVerdict(item)
				require.Equal(t, want, item.run(), "certificate %d", i)

				items := []verification{valid, item}
				results := make([]atomic.Bool, len(items))
				require.Equal(t, want, runVerificationChunk(items, results, 0, len(items), batch), "certificate %d", i)
			}
		})
	}
}
