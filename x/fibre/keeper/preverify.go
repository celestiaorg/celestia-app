package keeper

import (
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
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
	// StopOnFirstFailure abandons a round once a check in it fails. It skips
	// only items no worker has claimed yet, so it saves nothing where the items
	// are fewer than the workers - one transaction's quorum prefix, say.
	StopOnFirstFailure bool
}

// PreverifySignatures verifies the MsgPayForFibre signatures in txs across
// every CPU and records the successes in the signature cache. It decides
// nothing: the sequential transaction loop still performs and decides every
// check it leaves uncached.
func (k Keeper) PreverifySignatures(ctx sdk.Context, txs [][]byte, opts PreverifyOptions) {
	if k.sigCache == nil {
		return
	}

	work := k.collectVerifications(ctx, txs, opts)
	if len(work.promiseItems) == 0 && len(work.certItems) == 0 {
		return
	}

	// Promises first: a message whose promise fails is rejected before the
	// sequential verifier reaches its certificate, so verifying that
	// certificate is work no path needs.
	promiseVerified := runVerifications(work.promiseItems, opts.StopOnFirstFailure)
	certVerified := work.verifyCertificates(promiseVerified, opts.StopOnFirstFailure)
	work.record(k.sigCache, promiseVerified, certVerified)
}

// batchSize is how many validator signatures one work item verifies together.
// Batching amortises the fixed cost of the multiscalar multiplication; keeping
// items small still leaves far more of them than there are CPUs, so no core
// idles waiting on a transaction with few signatures.
const batchSize = 64

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
	// The pass needs one bit, not which entry failed, and Verify's fallback
	// re-verifies every entry serially to build a vector this discards. A
	// batch-equation false negative only costs a cache entry.
	if bv, ok := batch.(*ed25519.BatchVerifier); ok {
		return bv.VerifyBatchOnly(crypto.CReader())
	}
	allValid, _ := batch.Verify()
	return allValid
}

// verificationGroup is one message's contribution to the pass. Its keys are
// recorded together, and only when every item it queued succeeded: a message
// that fails a check the pass performed for it must buy no cache entry at all,
// or unverifiable spam could evict the entries of accepted messages.
type verificationGroup struct {
	keys []sigcache.Key
	// promiseIdx indexes promiseItems, or is -1 when the promise is cached.
	promiseIdx int
	// certFirst and certEnd bound this message's items in certItems.
	certFirst int
	certEnd   int
}

// preverification is the work collected for one block, split into the rounds it
// runs in: a certificate is verified only once its message's promise has been.
type preverification struct {
	promiseItems []verification
	certItems    []verification
	groups       []verificationGroup
}

// rollback drops the items group queued, so a message the pass abandons leaves
// no work behind.
func (p *preverification) rollback(group verificationGroup) {
	if group.promiseIdx >= 0 {
		p.promiseItems = p.promiseItems[:group.promiseIdx]
	}
	p.certItems = p.certItems[:group.certFirst]
}

// verifyCertificates runs the certificate items of every message whose promise
// passed, and reports the verdict per item. The items of a failed promise never
// run and stay false.
func (p *preverification) verifyCertificates(promiseVerified []bool, stopOnFirstFailure bool) []bool {
	verified := make([]bool, len(p.certItems))
	var (
		selected []int
		items    []verification
	)
	for _, group := range p.groups {
		if group.promiseIdx >= 0 && !promiseVerified[group.promiseIdx] {
			continue
		}
		for i := group.certFirst; i < group.certEnd; i++ {
			selected = append(selected, i)
			items = append(items, p.certItems[i])
		}
	}
	if len(items) == 0 {
		return verified
	}
	for i, ok := range runVerifications(items, stopOnFirstFailure) {
		verified[selected[i]] = ok
	}
	return verified
}

// record adds the cache entries of every group whose items all succeeded. Never
// cache the negatives: a false verdict also covers an item no worker claimed,
// and a batch-equation false negative, and both would reject a valid tx.
func (p *preverification) record(cache SigCache, promiseVerified, certVerified []bool) {
	for _, group := range p.groups {
		if group.promiseIdx >= 0 && !promiseVerified[group.promiseIdx] {
			continue
		}
		complete := true
		for i := group.certFirst; i < group.certEnd; i++ {
			if !certVerified[i] {
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
func (k Keeper) collectVerifications(ctx sdk.Context, txs [][]byte, opts PreverifyOptions) *preverification {
	work := &preverification{}
	// Promises in one block usually share a few heights; read and convert each
	// validator set once.
	valSets := make(map[int64]*convertedValidatorSet)

	for _, rawTx := range txs {
		msg, ok := types.ParsePayForFibreMsg(rawTx)
		if !ok {
			continue
		}

		promise := &fibre.PaymentPromise{}
		if err := promise.FromProto(&msg.PaymentPromise); err != nil {
			continue
		}
		promiseKey, keyed := promiseSigCacheKey(promise)
		if !keyed {
			continue
		}

		group := verificationGroup{promiseIdx: -1, certFirst: len(work.certItems)}
		group.certEnd = group.certFirst
		if !k.sigCache.Has(promiseKey) {
			group.promiseIdx = len(work.promiseItems)
			work.promiseItems = append(work.promiseItems, verification{promise: promise})
			group.keys = append(group.keys, promiseKey)
		}

		if opts.Certificates {
			certKey, err := msg.SigCacheKey()
			if err != nil {
				// A certificate the pass cannot name is one it cannot gate the
				// promise on. The sequential verifier still decides.
				work.rollback(group)
				continue
			}
			if !k.sigCache.Has(certKey) {
				if !k.appendCertificateItems(ctx, work, valSets, promise, msg) {
					// Returns false exactly where the sequential verifier
					// rejects the message, so this message records nothing -
					// its promise included.
					work.rollback(group)
					continue
				}
				group.certEnd = len(work.certItems)
				group.keys = append(group.keys, certKey)
			}
			// A cached certificate is already settled, so the promise is all
			// that is left to warm.
		}

		if len(group.keys) > 0 {
			work.groups = append(work.groups, group)
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
	promise *fibre.PaymentPromise,
	msg *types.MsgPayForFibre,
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

	signBytes, err := promise.SignBytes()
	if err != nil {
		return false
	}

	valSet := validator.Set{ValidatorSet: converted.set, Height: uint64(height)}
	minVotingPower := valSet.MinRequiredVotingPower(cmtmath.Fraction{Numerator: 2, Denominator: 3})
	prefix, quorumMet := validator.QuorumPrefix(converted.validators, msg.ValidatorSignatures, minVotingPower)
	if !quorumMet {
		return false
	}

	certFirst := len(work.certItems)
	batch := verification{message: signBytes}
	for i := range prefix {
		signature := msg.ValidatorSignatures[i]
		if len(signature) == 0 {
			continue
		}
		batch.pubKeys = append(batch.pubKeys, converted.validators[i].PubKey)
		batch.signatures = append(batch.signatures, signature)
		if len(batch.pubKeys) == batchSize {
			work.certItems = append(work.certItems, batch)
			batch = verification{message: signBytes}
		}
	}
	if len(batch.pubKeys) > 0 {
		work.certItems = append(work.certItems, batch)
	}
	// Guard against a certificate whose quorum prefix carried no signature at
	// all: recording its key would claim verification of nothing.
	return len(work.certItems) > certFirst
}

// runVerifications runs items across every CPU and reports which succeeded.
// Items are independent and each result is stored at its own index, so the
// outcome never depends on scheduling. An item that does not run stays false,
// which only costs a cache entry.
func runVerifications(items []verification, stopOnFirstFailure bool) []bool {
	results := make([]atomic.Bool, len(items))
	// GOMAXPROCS, not NumCPU: in a container limited to a fraction of the host
	// the latter would start goroutines that only contend for the same Ps.
	workers := min(runtime.GOMAXPROCS(0), len(items))

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
			for {
				i := int(next.Add(1)) - 1
				if i >= len(items) || (stopOnFirstFailure && aborted.Load()) {
					return
				}
				if items[i].run() {
					results[i].Store(true)
					continue
				}
				if stopOnFirstFailure {
					aborted.Store(true)
				}
			}
		})
	}
	wg.Wait()

	verified := make([]bool, len(items))
	for i := range results {
		verified[i] = results[i].Load()
	}
	return verified
}
