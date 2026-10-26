package rsema1d_test

import (
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d"
)

// TestVerifierLazyExtensionAcrossBlobs reuses one Verifier across two blobs.
// The parity RLC values are extended on the first parity row after each
// Verify, so the second blob's parity rows must not be checked against the
// first blob's extension.
func TestVerifierLazyExtensionAcrossBlobs(t *testing.T) {
	cfg := &rsema1d.Config{K: 64, N: 64, WorkerCount: 2}
	v, err := rsema1d.NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	r := rand.New(rand.NewPCG(31, 31))
	edA, commitA, rlcA := encodeRandom(t, r, cfg, 1024)
	edB, commitB, rlcB := encodeRandom(t, r, cfg, 1024)

	// blob A: originals and parity, so A's extension is built
	if err := v.Verify(commitA, rangeProofs(t, edA, 56, 72), rlcA); err != nil {
		t.Fatalf("Verify A: %v", err)
	}

	// blob B: originals only, then parity through VerifyShared
	if err := v.Verify(commitB, rangeProofs(t, edB, 0, 16), rlcB); err != nil {
		t.Fatalf("Verify B originals: %v", err)
	}
	if err := v.VerifyShared(commitB, rangeProofs(t, edB, 64, 80)); err != nil {
		t.Fatalf("VerifyShared B parity: %v", err)
	}

	// a tampered parity row is still rejected
	tampered := rangeProofs(t, edB, 80, 96)
	tampered[1].Row = append([]byte(nil), tampered[1].Row...)
	tampered[1].Row[0] ^= 0xFF
	if err := v.VerifyShared(commitB, tampered); err == nil {
		t.Fatal("VerifyShared accepted a tampered parity row")
	}
}

// TestVerifierVerifySharedConcurrentLazyExtension primes the Verifier with
// originals only, then races batches of originals against batches of parity
// that trigger the lazy extension. Run with -race.
func TestVerifierVerifySharedConcurrentLazyExtension(t *testing.T) {
	cfg := &rsema1d.Config{K: 64, N: 64, WorkerCount: 2}
	v, err := rsema1d.NewVerifier(cfg)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	r := rand.New(rand.NewPCG(37, 37))
	ed, commitment, rlcOrig := encodeRandom(t, r, cfg, 1024)

	if err := v.Verify(commitment, rangeProofs(t, ed, 0, 8), rlcOrig); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	const workers = 8
	batches := make([][]*rsema1d.RowProof, workers)
	for w := range batches {
		lo := w * 16 // workers 0-3 hold originals, 4-7 hold parity
		batches[w] = rangeProofs(t, ed, lo, lo+16)
	}

	var wg sync.WaitGroup
	errs := make([]error, workers)
	for w := range workers {
		wg.Go(func() {
			errs[w] = v.VerifyShared(commitment, batches[w])
		})
	}
	wg.Wait()
	for w, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: VerifyShared: %v", w, err)
		}
	}
}
