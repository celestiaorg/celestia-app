package keeper

import (
	"bytes"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestCertificateSplitSize(t *testing.T) {
	tests := map[string]struct {
		prefix, procs, want int
	}{
		"100-validator quorum, 16 procs": {prefix: 67, procs: 16, want: minSplitBatch},
		"100-validator quorum, 88 procs": {prefix: 67, procs: 88, want: minSplitBatch},
		"one proc keeps one batch":       {prefix: 67, procs: 1, want: 67},
		"large quorum, few procs":        {prefix: 134, procs: 4, want: 34},
		"never above batchSize":          {prefix: 400, procs: 2, want: batchSize},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, certificateSplitSize(tc.prefix, tc.procs))
		})
	}
}

// TestAppendCertificateItemsSplitsTheQuorumPrefix pins the partition: in order,
// exactly the prefix's non-empty signatures, cut into split-size items only when asked.
func TestAppendCertificateItemsSplitsTheQuorumPrefix(t *testing.T) {
	const height = 7
	validators := make([]*core.Validator, 100)
	signatures := make([][]byte, len(validators))
	for i := range validators {
		validators[i] = core.NewValidator(ed25519.GenPrivKey().PubKey(), 10)
		signatures[i] = bytes.Repeat([]byte{byte(i)}, ed25519.SignatureSize)
	}
	signatures[3], signatures[20] = nil, nil // the quorum prefix grows to 69
	set := core.NewValidatorSet(validators)
	set.TotalVotingPower()
	valSets := map[int64]*convertedValidatorSet{height: {validators: validators, set: set}}
	msg := &types.MsgPayForFibre{ValidatorSignatures: signatures}
	msg.PaymentPromise.Height = height
	var wantKeys []crypto.PubKey
	var wantSignatures [][]byte
	for i := range 69 {
		if len(signatures[i]) > 0 {
			wantKeys = append(wantKeys, validators[i].PubKey)
			wantSignatures = append(wantSignatures, signatures[i])
		}
	}

	tests := map[string]struct {
		split bool
		procs int
		want  []int
	}{
		"block pass keeps one item":   {split: false, procs: 16, want: []int{67}},
		"CheckTx on 16 procs":         {split: true, procs: 16, want: []int{8, 8, 8, 8, 8, 8, 8, 8, 3}},
		"CheckTx on 2 procs":          {split: true, procs: 2, want: []int{35, 32}},
		"CheckTx on 1 proc, one item": {split: true, procs: 1, want: []int{67}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(tc.procs))
			work := &preverification{}
			require.True(t, Keeper{}.appendCertificateItems(sdk.Context{}, work, valSets, []byte("sign bytes"), msg, tc.split))

			sizes := make([]int, 0, len(work.items))
			gotKeys := make([]crypto.PubKey, 0, 67)
			gotSignatures := make([][]byte, 0, 67)
			for _, item := range work.items {
				sizes = append(sizes, len(item.pubKeys))
				gotKeys = append(gotKeys, item.pubKeys...)
				gotSignatures = append(gotSignatures, item.signatures...)
			}
			require.Equal(t, tc.want, sizes)
			require.Equal(t, wantKeys, gotKeys)
			require.Equal(t, wantSignatures, gotSignatures)
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
		var verified []atomic.Bool
		require.NotPanics(t, func() {
			verified = runVerifications(items, stopOnFirstFailure, func([]atomic.Bool, int, int) {
				published.Add(1)
				panic("publish panic")
			})
		})
		require.Positive(t, published.Load(), "no worker panicked")
		for i := range verified {
			require.False(t, verified[i].Load() && !valid[i], "item %d: invalid signature reported verified", i)
		}
	}
}
