package keeper

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PreverifyOptions selects what a pre-verification pass covers.
type PreverifyOptions struct {
	// Certificates also verifies validator signature sets. FinalizeBlock never
	// checks them, so it leaves this off.
	Certificates bool
	// StopOnFirstFailure stops claiming work once a check fails. Set it where a
	// single bad signature rejects the whole block, so the rest is wasted.
	StopOnFirstFailure bool
	// splitCertificates spreads each certificate over several items, for a
	// pass too small to fill the CPUs with whole certificates.
	splitCertificates bool
}

// PreverifySignatures verifies the MsgPayForFibre signatures in txs across
// every CPU and records the successes in the signature cache. It decides
// nothing: the sequential transaction loop still performs and decides every
// check it leaves uncached.
func (k Keeper) PreverifySignatures(ctx sdk.Context, txs [][]byte, opts PreverifyOptions) {
	if k.sigCache == nil {
		return
	}
	decoded := make([]*types.DecodedPayForFibre, 0, len(txs))
	for _, rawTx := range txs {
		if d := DecodePayForFibre(rawTx); d != nil {
			decoded = append(decoded, d)
		}
	}
	k.PreverifyDecoded(ctx, decoded, opts)
}

// PreverifyDecoded is PreverifySignatures over txs the caller already decoded,
// so the phase decodes each tx once. nil entries are skipped.
func (k Keeper) PreverifyDecoded(ctx sdk.Context, decoded []*types.DecodedPayForFibre, opts PreverifyOptions) {
	if verify := k.PreparePreverification(ctx, decoded, opts); verify != nil {
		verify()
	}
}

// PreparePreverification collects state-dependent inputs synchronously and returns
// work that reads no SDK state, or nil when everything requested is cached or skipped.
func (k Keeper) PreparePreverification(ctx sdk.Context, decoded []*types.DecodedPayForFibre, opts PreverifyOptions) func() {
	if k.sigCache == nil {
		return nil
	}
	work := k.collectVerifications(ctx, decoded, opts)
	if len(work.items) == 0 {
		return nil
	}
	return func() {
		verified := runVerifications(work.items, opts.StopOnFirstFailure, func(results []atomic.Bool, start, end int) {
			work.record(k.sigCache, results, start, end)
		})
		// Groups crossing worker boundaries are published after all workers finish.
		work.record(k.sigCache, verified, 0, len(work.items))
	}
}

// PreverifyPayForFibreMsg runs the pass over one message the caller holds, and
// records it when every check passes. certKey must be msg's own key, or the
// entry would claim verification of a message the pass never saw.
func (k Keeper) PreverifyPayForFibreMsg(ctx sdk.Context, msg *types.MsgPayForFibre, certKey sigcache.Key) {
	if k.sigCache == nil {
		return
	}
	k.PreverifyDecoded(ctx, []*types.DecodedPayForFibre{DecodedFromMsg(msg, certKey)},
		PreverifyOptions{Certificates: true, StopOnFirstFailure: true, splitCertificates: true})
}

// batchSize is how many validator signatures one work item batch-verifies: 128
// covers a 100-validator quorum in one item and still leaves more items than CPUs.
const batchSize = 128

// minSplitBatch is the smallest sub-batch a split certificate gets: smaller
// batches lose more of the batch speed-up than the extra cores win back.
const minSplitBatch = 8

// certificateSplitSize spreads a prefix-signature certificate over procs items.
func certificateSplitSize(prefix, procs int) int {
	return min(batchSize, max(minSplitBatch, (prefix+procs-1)/procs))
}

// verification is one independent unit of work: either a payment promise's
// stateless validation, which carries its secp256k1 check, or a batch of
// validator ed25519 signatures over the same message.
type verification struct {
	promise    *fibre.PaymentPromise
	message    []byte
	pubKeys    []crypto.PubKey
	signatures [][]byte
}

// run reports whether every check in this item passed.
func (v verification) run() bool {
	if v.promise != nil {
		return v.promise.Validate() == nil
	}
	if len(v.pubKeys) == 1 {
		// A batch of one costs more than a plain verification.
		return v.pubKeys[0].VerifySignature(v.message, v.signatures[0])
	}

	batch := ed25519.NewBatchVerifier().(*ed25519.BatchVerifier)
	if !v.addTo(batch) {
		return false
	}
	return verifyBatchOnly(batch)
}

// verifyBatchOnly checks the batch equation alone: Verify's fallback re-verifies
// every entry serially for a vector the pass discards, on a rejection that
// replays for free. A false verdict only costs a cache entry.
func verifyBatchOnly(batch *ed25519.BatchVerifier) bool {
	return batch.VerifyBatchOnly(crypto.CReader())
}

// addTo queues the item's validator signatures on batch and reports whether
// it took every one.
func (v verification) addTo(batch crypto.BatchVerifier) bool {
	for i, pubKey := range v.pubKeys {
		if batch.Add(pubKey, v.message, v.signatures[i]) != nil {
			return false
		}
	}
	return true
}

// verificationGroup is one message's contribution to the pass. Its keys are
// recorded together, and only when every item in [first, end) succeeded: a
// message that fails a check the pass performed for it must buy no cache
// entry at all, or unverifiable spam could evict the entries of accepted
// messages.
type verificationGroup struct {
	keys  []sigcache.Key
	first int
	end   int
}

// preverification is the work collected for one block.
type preverification struct {
	items  []verification
	groups []verificationGroup
}

// record adds the cache entries of every group whose items all succeeded and
// that lies wholly inside [start, end).
func (p *preverification) record(cache SigCache, verified []atomic.Bool, start, end int) {
	first := sort.Search(len(p.groups), func(i int) bool { return p.groups[i].first >= start })
	for _, group := range p.groups[first:] {
		if group.first >= end {
			break
		}
		if group.end > end {
			continue
		}
		complete := true
		for i := group.first; i < group.end; i++ {
			if !verified[i].Load() {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		for _, key := range group.keys {
			cache.Add(key)
		}
	}
}

// collectVerifications builds the work queue. Every state read happens here,
// sequentially on ctx, so the workers touch nothing but their own inputs.
func (k Keeper) collectVerifications(ctx sdk.Context, decoded []*types.DecodedPayForFibre, opts PreverifyOptions) *preverification {
	// Backstop for a caller that does not bound its input. Both proposal
	// callers already do, so this never truncates a legitimate pass.
	covered := 0

	work := &preverification{}
	// Promises in one block usually share a few heights; read and convert each
	// validator set once.
	valSets := make(map[int64]*convertedValidatorSet)
	// Collect each distinct certificate once: a repeat carries the same promise,
	// so the first occurrence warms both keys. Sized by the pass's message cap.
	var seenCerts map[sigcache.Key]struct{}
	if opts.Certificates && len(decoded) > 1 {
		seenCerts = make(map[sigcache.Key]struct{}, min(len(decoded), appconsts.MaxPayForFibreCandidates))
	}

	for _, d := range decoded {
		if d == nil || !d.PromiseKeyed() {
			continue
		}
		if covered >= appconsts.MaxPayForFibreCandidates {
			break
		}
		if seenCerts != nil && d.CertKeyed {
			if _, seen := seenCerts[d.CertKey]; seen {
				continue
			}
			seenCerts[d.CertKey] = struct{}{}
		}

		msg := d.Msg

		promise := &fibre.PaymentPromise{}
		if err := promise.FromProto(&msg.PaymentPromise); err != nil {
			continue
		}

		// Count only what the pass takes on, not what it skipped.
		covered++

		first := len(work.items)
		var keys []sigcache.Key
		if !k.sigCache.Has(d.PromiseKey) {
			work.items = append(work.items, verification{promise: promise})
			keys = append(keys, d.PromiseKey)
		}

		if opts.Certificates {
			if !d.CertKeyed {
				// A certificate the pass cannot name is one it cannot gate the
				// promise on. The sequential verifier still decides.
				work.items = work.items[:first]
				continue
			}
			if !k.sigCache.Has(d.CertKey) {
				if !k.appendCertificateItems(ctx, work, valSets, d.PromiseSignBytes, msg, opts.splitCertificates) {
					// Returns false exactly where the sequential verifier
					// rejects the message, so this message records nothing -
					// its promise included.
					work.items = work.items[:first]
					continue
				}
				keys = append(keys, d.CertKey)
			}
			// A cached certificate is already settled, so the promise is all
			// that is left to warm.
		}

		if len(keys) > 0 {
			work.groups = append(work.groups, verificationGroup{
				keys: keys, first: first, end: len(work.items),
			})
		}
	}
	return work
}

// appendCertificateItems queues one ed25519 check per signature in the quorum
// prefix and reports whether the certificate can be cached if they all pass. It
// returns false whenever the sequential verifier would reject the message, so
// the error stays that verifier's to report.
func (k Keeper) appendCertificateItems(
	ctx sdk.Context,
	work *preverification,
	valSets map[int64]*convertedValidatorSet,
	signBytes []byte,
	msg *types.MsgPayForFibre,
	split bool,
) bool {
	height := msg.PaymentPromise.Height
	converted, ok := valSets[height]
	if !ok {
		var err error
		if converted, err = k.certificateValidatorSet(ctx, height); err != nil {
			return false
		}
		valSets[height] = converted
	}
	if checkSignatureCount(converted, msg.ValidatorSignatures) != nil {
		return false
	}

	valSet := validator.Set{ValidatorSet: converted.set, Height: uint64(height)}
	minVotingPower := valSet.MinRequiredVotingPower(quorumFraction)
	prefix, quorumMet := validator.QuorumPrefix(converted.validators, msg.ValidatorSignatures, minVotingPower)
	if !quorumMet {
		return false
	}

	size := batchSize
	if split {
		size = certificateSplitSize(prefix, runtime.GOMAXPROCS(0))
	}
	certFirst := len(work.items)
	newBatch := func(remaining int) verification {
		n := min(remaining, size)
		return verification{message: signBytes, pubKeys: make([]crypto.PubKey, 0, n), signatures: make([][]byte, 0, n)}
	}
	batch := newBatch(prefix)
	for i := range prefix {
		signature := msg.ValidatorSignatures[i]
		if len(signature) == 0 {
			continue
		}
		batch.pubKeys = append(batch.pubKeys, converted.validators[i].PubKey)
		batch.signatures = append(batch.signatures, signature)
		if len(batch.pubKeys) == size {
			work.items = append(work.items, batch)
			batch = newBatch(prefix - i - 1)
		}
	}
	if len(batch.pubKeys) > 0 {
		work.items = append(work.items, batch)
	}
	// Guard against a certificate whose quorum prefix carried no signature at
	// all: recording its key would claim verification of nothing. certFirst is
	// past the promise item, so only certificate items count.
	return len(work.items) > certFirst
}

// runVerifications runs items across every CPU and reports which succeeded.
// Items are independent and each result is stored at its own index, so the
// outcome never depends on scheduling. An item that does not run stays false,
// which only costs a cache entry.
func runVerifications(items []verification, stopOnFirstFailure bool, publish func([]atomic.Bool, int, int)) []atomic.Bool {
	// atomic.Bool, not []bool: workers write adjacent results, and packed bools
	// false-share one cache line (about 1% of ProcessProposal, measured).
	results := make([]atomic.Bool, len(items))
	if len(items) == 1 {
		// One item has nothing to spread; the fan-out would be pure overhead.
		results[0].Store(items[0].run())
		return results
	}

	// GOMAXPROCS, not NumCPU: in a container limited to a fraction of the host
	// the latter would start goroutines that only contend for the same Ps.
	procs := runtime.GOMAXPROCS(0)
	chunkSize := verificationChunkSize(len(items), procs)
	workers := min(procs, (len(items)+chunkSize-1)/chunkSize)

	var (
		next    atomic.Int64
		aborted atomic.Bool
		wg      sync.WaitGroup
	)
	for range workers {
		wg.Go(func() {
			// A worker panic must not take the node down: its unfinished items stay unverified
			// and the sequential path redoes them.
			defer func() { _ = recover() }()
			// NewBatchVerifier always returns *BatchVerifier; otherwise every item stays
			// unverified, which the preverify tests catch as a cache that never warms.
			batch := ed25519.NewBatchVerifier().(*ed25519.BatchVerifier)
			for {
				i := int(next.Add(int64(chunkSize))) - chunkSize
				if i >= len(items) || (stopOnFirstFailure && aborted.Load()) {
					return
				}
				end := min(i+chunkSize, len(items))
				if !runVerificationChunk(items, results, i, end, batch) && stopOnFirstFailure {
					aborted.Store(true)
				}
				publish(results, i, end)
			}
		})
	}
	wg.Wait()
	return results
}

// maxChunkSize is how many items one worker claims, and combines into one
// ed25519 batch, at a time.
const maxChunkSize = 8

// verificationChunkSize shrinks chunks until every proc has one, so CheckTx's
// promise and certificate still verify in parallel.
func verificationChunkSize(items, procs int) int {
	return min(maxChunkSize, max(1, (items+procs-1)/procs))
}

// runVerificationChunk combines a few certificate batches; a failed combined batch
// leaves their entries unset for the ordered path, which still decides.
func runVerificationChunk(items []verification, results []atomic.Bool, start, end int, batch *ed25519.BatchVerifier) bool {
	batch.Reset()
	allValid := true
	batchReady := true
	certificates := 0
	for i := start; i < end; i++ {
		item := items[i]
		if item.promise != nil {
			if item.run() {
				results[i].Store(true)
			} else {
				allValid = false
			}
			continue
		}
		certificates++
		batchReady = batchReady && item.addTo(batch)
	}
	if certificates == 0 {
		return allValid
	}
	if batchReady {
		if verifyBatchOnly(batch) {
			for i := start; i < end; i++ {
				if items[i].promise == nil {
					results[i].Store(true)
				}
			}
			return allValid
		}
	}
	return false
}
