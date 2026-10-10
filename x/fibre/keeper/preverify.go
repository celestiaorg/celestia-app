package keeper

import (
	"runtime"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/ed25519batch"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
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
	// SeenCertificates optionally deduplicates collected certificates. The
	// caller supplies an empty map, populated synchronously during collection.
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
	verify := k.PreparePreverification(ctx, decoded, opts)
	if verify == nil {
		return
	}
	verify()
}

// PreparePreverification collects state-dependent inputs synchronously. The
// returned work reads no SDK state and may run alongside the ordered ante walk.
// A nil function means all requested verifications were already cached or skipped.
func (k Keeper) PreparePreverification(ctx sdk.Context, decoded []*types.DecodedPayForFibre, opts PreverifyOptions) func() {
	if k.sigCache == nil {
		return nil
	}
	work := k.collectVerifications(ctx, decoded, opts)
	if len(work.items) == 0 {
		return nil
	}
	return func() {
		verified := runVerifications(work.items, opts.StopOnFirstFailure, func(verified []bool, start, end int) {
			work.record(k.sigCache, verified, start, end)
		})
		// Groups crossing worker boundaries are published after all workers finish.
		work.record(k.sigCache, verified, 0, len(work.items))
	}
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

// verificationGroup is a cache entry that may be recorded once every item in
// [first, end) succeeded.
type verificationGroup struct {
	key   sigcache.Key
	first int
	end   int
}

// preverification is the work collected for one block.
type preverification struct {
	items  []verification
	groups []verificationGroup
}

// record adds the cache entry of every group whose items all succeeded.
func (p *preverification) record(cache SigCache, verified []bool, start, end int) {
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
			if !verified[i] {
				complete = false
				break
			}
		}
		if complete {
			cache.Add(group.key)
		}
	}
}

// collectVerifications builds the work queue. Every state read happens here,
// sequentially on ctx, so the workers touch nothing but their own inputs.
func (k Keeper) collectVerifications(ctx sdk.Context, decoded []*types.DecodedPayForFibre, opts PreverifyOptions) *preverification {
	// A block carries at most this many PayForFibre messages, so the pass never
	// covers more. PrepareProposal is handed everything the mempool offers
	// within block max bytes, which is far more than any block can include.
	limit := appconsts.MaxPayForFibreMessages
	covered := 0

	work := &preverification{}
	// Promises in one block usually share a few heights; read and convert each
	// validator set once.
	valSets := make(map[int64]*convertedValidatorSet)

	for _, d := range decoded {
		if d == nil || !d.PromiseKeyed {
			continue
		}
		if covered >= limit {
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

		// Count only what the pass actually takes on. Spending the budget on
		// entries it skipped would starve the ones that need verifying.
		covered++

		promiseCached := k.sigCache.Has(d.PromiseKey)
		first := len(work.items)
		if !promiseCached {
			promise := &fibre.PaymentPromise{}
			if err := promise.FromProto(&msg.PaymentPromise); err != nil {
				continue
			}
			if work.items == nil {
				capacity := 2 * min(len(decoded), limit)
				work.items = make([]verification, 0, capacity)
				work.groups = make([]verificationGroup, 0, capacity)
			}
			work.items = append(work.items, verification{promise: promise})
			work.groups = append(work.groups, verificationGroup{key: d.PromiseKey, first: first, end: first + 1})
		}

		if !opts.Certificates || !d.CertKeyed || k.sigCache.Has(d.CertKey) {
			continue
		}
		if k.appendCertificateItems(ctx, work, valSets, d.PromiseSignBytes, msg, first) {
			work.groups = append(work.groups, verificationGroup{key: d.CertKey, first: first, end: len(work.items)})
		}
	}
	return work
}

// hasPreverificationGas reports whether the transaction declared enough gas for
// the signature checks this pass would run for it. It is a lower bound: the
// ante handler charges for more than signatures and remains the authority on
// whether the transaction can pay.
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

	// A contiguous quorum fitting one item can borrow both immutable slices.
	if prefix > 0 && prefix <= batchSize {
		dense := true
		for _, signature := range msg.ValidatorSignatures[:prefix] {
			if len(signature) == 0 {
				dense = false
				break
			}
		}
		if dense {
			work.items = append(work.items, verification{
				message: signBytes, pubKeys: converted.publicKeys[:prefix], signatures: msg.ValidatorSignatures[:prefix],
			})
			return true
		}
	}
	certFirst := len(work.items)
	batch := verification{
		message:    signBytes,
		pubKeys:    make([]crypto.PubKey, 0, min(prefix, batchSize)),
		signatures: make([][]byte, 0, min(prefix, batchSize)),
	}
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
func runVerifications(items []verification, stopOnFirstFailure bool, publish func([]bool, int, int)) []bool {
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
			batch := new(ed25519batch.Verifier)
			for {
				i := int(next.Add(chunkSize)) - chunkSize
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

// runVerificationChunk combines a few certificate batches. A failed combined
// batch leaves their cache entries unset; the ordered path verifies them again
// and remains the authority on the block's validity.
func runVerificationChunk(items []verification, results []bool, start, end int, batch *ed25519batch.Verifier) bool {
	if batch == nil {
		// The per-item path the caller's comma-ok assertion falls back to:
		// slower, same verdicts, and it keeps a failed assertion from turning
		// every chunk into a nil dereference.
		allValid := true
		for i := start; i < end; i++ {
			item := items[i]
			ok := true
			if item.promise != nil {
				ok = verifyPromise(item.promise)
			} else {
				for j, key := range item.pubKeys {
					if !key.VerifySignature(item.message, item.signatures[j]) {
						ok = false
						break
					}
				}
			}
			if ok {
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
			if verifyPromise(item.promise) {
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
		if batch.VerifyBatchOnly(crypto.CReader()) {
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
