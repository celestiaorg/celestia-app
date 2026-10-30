package keeper_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/stretchr/testify/require"
)

// maxPreverifyMutations bounds the edits applied to one batch so each input
// does a bounded amount of work.
const maxPreverifyMutations = 8

// mutatePreverifyBatch applies up to maxPreverifyMutations edits to txs. Each
// edit consumes four bytes of ops: tx index, operation and a 16-bit offset.
func mutatePreverifyBatch(txs [][]byte, ops, extra []byte) [][]byte {
	out := make([][]byte, len(txs))
	for i, tx := range txs {
		out[i] = bytes.Clone(tx)
	}
	for n := 0; n+4 <= len(ops) && n/4 < maxPreverifyMutations && len(out) > 0; n += 4 {
		i := int(ops[n]) % len(out)
		tx := out[i]
		at := 0
		if len(tx) > 0 {
			at = (int(ops[n+2])<<8 | int(ops[n+3])) % len(tx)
		}
		switch ops[n+1] % 8 {
		case 0:
			if len(tx) > 0 {
				tx[at] ^= 1 << (ops[n+3] % 8)
			}
		case 1:
			if len(tx) > 0 {
				tx[at] = ops[n+2]
			}
		case 2:
			out[i] = tx[:at]
		case 3:
			out[i] = append(append(append([]byte{}, tx[:at]...), extra...), tx[at:]...)
		case 4:
			end := min(at+1+len(extra), len(tx))
			out[i] = append(append([]byte{}, tx[:at]...), tx[end:]...)
		case 5:
			out = append(out, bytes.Clone(tx))
		case 6:
			out[i] = bytes.Clone(extra)
		case 7:
			out = append(out[:i], out[i+1:]...)
		}
	}
	return out
}

// fuzzPreverify runs the signature pre-pass on a block of mutated
// pay-for-fibre txs and checks that every cache entry it leaves is one the
// sequential verifier would also accept. A cache entry skips that verifier on
// this node only, so an unsound entry lets nodes with different caches vote
// differently on the same block.
func fuzzPreverify(t *testing.T, ops, extra []byte, certificates, stopOnFirstFailure bool) {
	t.Helper()
	f := newPreverifyFixture(t)
	txs := mutatePreverifyBatch([][]byte{f.txBytes(t), f.txBytesFor(t, f.signedMessage(t, 1))}, ops, extra)

	prepass := f.withFreshCache(t)
	prepass.keeper.PreverifySignatures(prepass.ctx, txs, keeper.PreverifyOptions{
		Certificates:       certificates,
		StopOnFirstFailure: stopOnFirstFailure,
	})

	accounted := make(map[sigcache.Key]struct{})
	for _, raw := range txs {
		d := keeper.DecodePayForFibre(raw)
		if d == nil {
			continue
		}
		if d.PromiseKeyed() && prepass.cache.Has(d.PromiseKey) {
			pp := fibre.PaymentPromise{}
			require.NoError(t, pp.FromProto(&d.Msg.PaymentPromise), "a recorded promise must convert")
			require.NoError(t, f.withFreshCache(t).keeper.ValidatePromiseStateless(&pp),
				"the pre-pass recorded a promise the sequential verifier rejects")
			accounted[d.PromiseKey] = struct{}{}
		}
		if d.CertKeyed && prepass.cache.Has(d.CertKey) {
			require.True(t, certificates, "certificates were recorded although not requested")
			reference := f.withFreshCache(t)
			require.NoError(t, reference.keeper.ValidatePayForFibreSignatures(reference.ctx, d.Msg),
				"the pre-pass recorded a certificate the sequential verifier rejects")
			accounted[d.CertKey] = struct{}{}
		}
	}
	require.Equal(t, len(accounted), prepass.cache.Len(), "the cache holds an entry no tx in the block accounts for")
}

// TestPreverifyFuzzSeeds keeps the fuzz seeds meaningful: the unmodified block
// warms every key, and each single mutation runs without a failure.
func TestPreverifyFuzzSeeds(t *testing.T) {
	f := newPreverifyFixture(t)
	f.keeper.PreverifySignatures(f.ctx, [][]byte{f.txBytes(t)}, keeper.PreverifyOptions{Certificates: true})
	require.Equal(t, 2, f.cache.Len(), "the unmodified tx must warm its promise and certificate")

	for op := range byte(8) {
		for txIndex := range byte(2) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				fuzzPreverify(t, []byte{txIndex, op, 0, 40}, []byte{0x0a, 0x00}, true, false)
			})
		}
	}
}

// FuzzPreverifySignatures mutates a block of valid pay-for-fibre txs and runs
// the parallel signature pre-pass that PrepareProposal, ProcessProposal and
// FinalizeBlock use. The PreBlocker call runs without a panic recovery, so any
// panic halts the node; every cache entry must be one the sequential verifier
// accepts, and nothing else may enter the cache.
func FuzzPreverifySignatures(f *testing.F) {
	f.Add([]byte{}, []byte{}, true, false)
	f.Add([]byte{}, []byte{}, false, false)
	for op := range byte(8) {
		for txIndex := range byte(2) {
			for _, offset := range [][2]byte{{0, 7}, {0, 120}, {1, 0}} {
				f.Add([]byte{txIndex, op, offset[0], offset[1]}, []byte{0x0a, 0x00}, true, op%2 == 0)
			}
		}
	}
	// Duplicate a tx, then corrupt one copy's signatures.
	f.Add([]byte{0, 5, 0, 0, 2, 0, 0, 200, 1, 1, 0, 180}, []byte{}, true, true)
	f.Fuzz(func(t *testing.T, ops, extra []byte, certificates, stopOnFirstFailure bool) {
		if len(extra) > 1<<16 {
			return
		}
		fuzzPreverify(t, ops, extra, certificates, stopOnFirstFailure)
	})
}
