package keeper

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
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
	// SeenCertificates skips certificates already collected, so a repeated
	// certificate costs one verification. The caller supplies an empty map.
	SeenCertificates map[sigcache.Key]struct{}
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
	if k.sigCache == nil {
		return
	}

	work := k.collectVerifications(ctx, decoded, opts)
	if len(work.items) == 0 {
		return
	}
	work.record(k.sigCache, runVerifications(work.items, opts.StopOnFirstFailure))
}

// PreverifyPayForFibreMsg runs the pass over one message the caller holds, and
// records it when every check passes. certKey must be msg's own key, or the
// entry would claim verification of a message the pass never saw.
func (k Keeper) PreverifyPayForFibreMsg(ctx sdk.Context, msg *types.MsgPayForFibre, certKey sigcache.Key) {
	if k.sigCache == nil {
		return
	}
	k.PreverifyDecoded(ctx, []*types.DecodedPayForFibre{DecodedFromMsg(msg, certKey)},
		PreverifyOptions{Certificates: true, StopOnFirstFailure: true})
}

// batchSize is how many validator signatures one work item verifies together.
// Batching amortises the fixed cost of the multiscalar multiplication; keeping
// items small still leaves far more of them than there are CPUs, so no core
// idles waiting on a transaction with few signatures.
const batchSize = 128

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

	batch := ed25519.NewBatchVerifier()
	for i, pubKey := range v.pubKeys {
		if batch.Add(pubKey, v.message, v.signatures[i]) != nil {
			return false
		}
	}
	// A failing batch falls back to verifying each entry, so the verdict is the
	// same one the sequential walk reaches.
	allValid, _ := batch.Verify()
	return allValid
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

// record adds the cache entries of every group whose items all succeeded.
func (p *preverification) record(cache SigCache, verified []bool) {
	for _, group := range p.groups {
		complete := true
		for i := group.first; i < group.end; i++ {
			if !verified[i] {
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

	for _, d := range decoded {
		if d == nil || !d.PromiseKeyed {
			continue
		}
		if covered >= appconsts.MaxPayForFibreCandidates {
			break
		}
		if opts.SeenCertificates != nil && d.CertKeyed {
			if _, seen := opts.SeenCertificates[d.CertKey]; seen {
				continue
			}
			opts.SeenCertificates[d.CertKey] = struct{}{}
		}

		// A pre-pass must not run signature work a transaction cannot pay for.
		// Skipping only leaves the authoritative sequential check uncached.
		if !hasPreverificationGas(d) {
			continue
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
				if !k.appendCertificateItems(ctx, work, valSets, d.PromiseSignBytes, msg, first) {
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

// hasPreverificationGas reports whether the tx declared enough gas for the
// signature checks this pass would run. A lower bound: the ante handler
// charges for more and stays the authority on whether the tx can pay.
func hasPreverificationGas(d *types.DecodedPayForFibre) bool {
	return d.GasLimit >= types.EstimateGasForPayForFibreSignatureVerification(uint64(len(d.Msg.ValidatorSignatures)))
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
	first int,
) bool {
	height := msg.PaymentPromise.Height
	converted, ok := valSets[height]
	if !ok {
		historicalInfo, err := k.stakingKeeper.GetHistoricalInfo(ctx, height)
		if err != nil {
			return false
		}
		if converted, err = k.validatorSetAtHeight(height, historicalInfo.Valset); err != nil {
			return false
		}
		valSets[height] = converted
	}

	// The list is positional over the validator set, so more entries than
	// validators is malformed.
	if len(msg.ValidatorSignatures) > len(converted.validators) {
		return false
	}

	valSet := validator.Set{ValidatorSet: converted.set, Height: uint64(height)}
	minVotingPower := valSet.MinRequiredVotingPower(cmtmath.Fraction{Numerator: 2, Denominator: 3})
	prefix, quorumMet := validator.QuorumPrefix(converted.validators, msg.ValidatorSignatures, minVotingPower)
	if !quorumMet {
		return false
	}

	certFirst := len(work.items)
	batch := verification{message: signBytes}
	for i := range prefix {
		signature := msg.ValidatorSignatures[i]
		if len(signature) == 0 {
			continue
		}
		batch.pubKeys = append(batch.pubKeys, converted.validators[i].PubKey)
		batch.signatures = append(batch.signatures, signature)
		if len(batch.pubKeys) == batchSize {
			work.items = append(work.items, batch)
			batch = verification{message: signBytes}
		}
	}
	if len(batch.pubKeys) > 0 {
		work.items = append(work.items, batch)
	}
	// Guard against a certificate whose quorum prefix carried no signature at
	// all: recording its key would claim verification of nothing. first is the
	// promise item, which is why the count starts after it.
	return len(work.items) > certFirst
}

// runVerifications runs items across every CPU and reports which succeeded.
// Items are independent and each result is stored at its own index, so the
// outcome never depends on scheduling. An item that does not run stays false,
// which only costs a cache entry.
func runVerifications(items []verification, stopOnFirstFailure bool) []bool {
	results := make([]bool, len(items))
	const chunkSize = 8
	// GOMAXPROCS, not NumCPU: in a container limited to a fraction of the host
	// the latter would start goroutines that only contend for the same Ps.
	workers := min(runtime.GOMAXPROCS(0), (len(items)+chunkSize-1)/chunkSize)

	var (
		next     atomic.Int64
		aborted  atomic.Bool
		panicked atomic.Value
		wg       sync.WaitGroup
	)
	for range workers {
		wg.Go(func() {
			// Nothing here should panic, but a worker that does must not take
			// the node down: its unfinished items stay unverified and the
			// sequential path redoes them. The value is kept so the caller can
			// report it - a recurring panic would otherwise show up only as a
			// cache that never warms.
			defer func() {
				if r := recover(); r != nil {
					panicked.CompareAndSwap(nil, r)
				}
			}()
			// Comma-ok: a failed assertion would panic every worker and show up
			// only as a cache that never warms. Fall back to the per-item path.
			batch, batchOK := ed25519.NewBatchVerifier().(*ed25519.BatchVerifier)
			if !batchOK {
				batch = nil
			}
			for {
				i := int(next.Add(chunkSize)) - chunkSize
				if i >= len(items) || (stopOnFirstFailure && aborted.Load()) {
					return
				}
				if !runVerificationChunk(items, results, i, min(i+chunkSize, len(items)), batch) && stopOnFirstFailure {
					aborted.Store(true)
				}
			}
		})
	}
	wg.Wait()

	return results
}

// runVerificationChunk combines a few certificate batches. A failed combined
// batch leaves their cache entries unset; the ordered path verifies them again
// and remains the authority on the block's validity.
func runVerificationChunk(items []verification, results []bool, start, end int, batch *ed25519.BatchVerifier) bool {
	if batch == nil {
		// The per-item path the caller's comma-ok assertion falls back to.
		allValid := true
		for i := start; i < end; i++ {
			if items[i].run() {
				results[i] = true
				continue
			}
			allValid = false
		}
		return allValid
	}
	batch.Reset()
	allValid := true
	batchReady := true
	certificates := 0
	for i := start; i < end; i++ {
		item := items[i]
		if item.promise != nil {
			if item.run() {
				results[i] = true
			} else {
				allValid = false
			}
			continue
		}
		certificates++
		if !batchReady {
			continue
		}
		for j, pubKey := range item.pubKeys {
			if batch.Add(pubKey, item.message, item.signatures[j]) != nil {
				batchReady = false
				break
			}
		}
	}
	if certificates == 0 {
		return allValid
	}
	if batchReady {
		if valid, _ := batch.Verify(); valid {
			for i := start; i < end; i++ {
				if items[i].promise == nil {
					results[i] = true
				}
			}
			return allValid
		}
	}
	return false
}
