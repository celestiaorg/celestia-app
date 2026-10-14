package keeper

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/stretchr/testify/require"
)

// differentialCertificates is how many independent certificates each class of
// the differential test below checks.
const differentialCertificates = 100

// newCertificateItem returns a work item of size signatures over message, all
// of them valid, along with the keys that signed it.
func newCertificateItem(t testing.TB, message []byte, signatures int) (verification, []ed25519.PrivKey) {
	t.Helper()

	item := verification{message: message}
	privKeys := make([]ed25519.PrivKey, signatures)
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

// batchVerifyVerdict is the verdict the pass reached before it stopped at the
// batch equation: Verify's allValid, which falls back to verifying every entry
// serially.
func batchVerifyVerdict(item verification) bool {
	batch := ed25519.NewBatchVerifier()
	for i, pubKey := range item.pubKeys {
		if batch.Add(pubKey, item.message, item.signatures[i]) != nil {
			return false
		}
	}
	allValid, _ := batch.Verify()
	return allValid
}

// TestVerificationRunMatchesTheSequentialVerdict checks that deciding a batch on
// the batch equation alone reports the verdict the sequential verifier reaches,
// and the one the serial fallback used to report, for every shape of input.
func TestVerificationRunMatchesTheSequentialVerdict(t *testing.T) {
	message := []byte("payment promise sign bytes")
	other := []byte("sign bytes of another promise")

	// Forged, not malformed: random bytes are rejected before any curve
	// arithmetic, so they would not exercise the equation at all.
	forge := func(t testing.TB, item *verification, privKeys []ed25519.PrivKey, index int) {
		t.Helper()

		signature, err := privKeys[index].Sign(other)
		require.NoError(t, err)
		item.signatures[index] = signature
	}

	tests := map[string]func(t testing.TB, item *verification, privKeys []ed25519.PrivKey){
		"all signatures valid": func(testing.TB, *verification, []ed25519.PrivKey) {},
		"forged at the first index": func(t testing.TB, item *verification, privKeys []ed25519.PrivKey) {
			forge(t, item, privKeys, 0)
		},
		"forged in the middle": func(t testing.TB, item *verification, privKeys []ed25519.PrivKey) {
			forge(t, item, privKeys, batchSize/2-1)
		},
		"forged at the last index": func(t testing.TB, item *verification, privKeys []ed25519.PrivKey) {
			forge(t, item, privKeys, batchSize-1)
		},
		"non-canonical signature": func(_ testing.TB, item *verification, _ []ed25519.PrivKey) {
			// Fails canonicity rather than the equation, the branch that
			// aborts before the multiscalar multiplication.
			item.signatures[7] = bytes.Repeat([]byte{0xff}, ed25519.SignatureSize)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			for i := range differentialCertificates {
				item, privKeys := newCertificateItem(t, message, batchSize)
				mutate(t, &item, privKeys)

				want := sequentialVerdict(item)
				require.Equal(t, want, item.run(), "certificate %d", i)
				require.Equal(t, want, batchVerifyVerdict(item), "certificate %d", i)
			}
		})
	}
}

// TestVerificationRunOnAnEmptyBatch pins the one shape where the equation and a
// sequential walk disagree: an empty batch, which the walk vacuously accepts.
// The pass never builds one, and both verdicts here are false anyway.
func TestVerificationRunOnAnEmptyBatch(t *testing.T) {
	item := verification{message: []byte("payment promise sign bytes")}

	require.False(t, item.run())
	require.False(t, batchVerifyVerdict(item))
}

// TestVerifyCertificatesWaitsForThePromise pins the two rounds: a message whose
// promise fails is rejected before the sequential verifier reaches its
// certificate, so the pass must not verify that certificate at all.
func TestVerifyCertificatesWaitsForThePromise(t *testing.T) {
	message := []byte("payment promise sign bytes")
	item, _ := newCertificateItem(t, message, batchSize)

	failed := sigcache.NewKey(sigcache.PffCertificate, []byte("rejected"))
	passed := sigcache.NewKey(sigcache.PffCertificate, []byte("accepted"))
	work := &preverification{
		// An empty promise fails its stateless validation.
		promiseItems: []verification{{promise: &fibre.PaymentPromise{}}},
		certItems:    []verification{item, item},
		groups: []verificationGroup{
			{keys: []sigcache.Key{failed}, promiseIdx: 0, certFirst: 0, certEnd: 1},
			// A message whose promise is already cached has nothing to wait
			// for, so its certificate must still be verified.
			{keys: []sigcache.Key{passed}, promiseIdx: -1, certFirst: 1, certEnd: 2},
		},
	}

	promiseVerified := runVerifications(work.promiseItems, false)
	require.False(t, promiseVerified[0])

	certVerified := work.verifyCertificates(promiseVerified, false)
	// Both items carry the same valid signatures, so a false verdict on the
	// first can only mean it never ran.
	require.False(t, certVerified[0], "a failed promise must leave its certificate unverified")
	require.True(t, certVerified[1], "and must not hold back another message's")

	cache := sigcache.New(8)
	work.record(cache, promiseVerified, certVerified)
	require.False(t, cache.Has(failed), "a failed promise must buy no entry")
	require.True(t, cache.Has(passed))
}
