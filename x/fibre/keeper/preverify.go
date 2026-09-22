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
	// StopOnFirstFailure stops claiming work once a check fails. Set it where a
	// single bad signature rejects the whole block, so the rest is wasted.
	StopOnFirstFailure bool
}

// PreverifySignatures verifies the MsgPayForFibre signatures in txs before the
// sequential transaction loop reaches them, spreading the work across every
// CPU, and records the successes in the signature cache.
//
// It decides nothing and returns nothing. A check that fails is left out of the
// cache, so the loop performs it again and produces the authoritative error;
// whether this ran cannot change any outcome. Each result is stored at its own
// index and only complete groups are recorded, so scheduling cannot affect the
// cache either.
func (k Keeper) PreverifySignatures(ctx sdk.Context, txs [][]byte, opts PreverifyOptions) {
	if k.sigCache == nil {
		return
	}

	work := k.collectVerifications(ctx, txs, opts)
	if len(work.items) == 0 {
		return
	}
	work.record(k.sigCache, runVerifications(work.items, opts.StopOnFirstFailure))
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
	// A failing batch falls back to verifying each entry, so the verdict is the
	// same one the sequential walk reaches.
	allValid, _ := batch.Verify()
	return allValid
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
func (p *preverification) record(cache SigCache, verified []bool) {
	for _, group := range p.groups {
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

		promiseCached := k.sigCache.Has(promiseKey)
		first := len(work.items)
		if !promiseCached {
			work.items = append(work.items, verification{promise: promise})
			work.groups = append(work.groups, verificationGroup{key: promiseKey, first: first, end: first + 1})
		}

		if !opts.Certificates {
			continue
		}
		certKey, err := msg.SigCacheKey()
		if err != nil || k.sigCache.Has(certKey) {
			continue
		}
		if k.appendCertificateItems(ctx, work, valSets, promise, msg, first) {
			work.groups = append(work.groups, verificationGroup{key: certKey, first: first, end: len(work.items)})
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
	// Guard against a caller that queued nothing at all for this message.
	return len(work.items) > first
}

// runVerifications runs items across every CPU and reports which succeeded.
// Items are independent and each result is stored at its own index, so the
// outcome never depends on scheduling. An item that does not run stays false,
// which only costs a cache entry.
func runVerifications(items []verification, stopOnFirstFailure bool) []bool {
	results := make([]atomic.Bool, len(items))
	workers := min(runtime.NumCPU(), len(items))

	var (
		next    atomic.Int64
		aborted atomic.Bool
		wg      sync.WaitGroup
	)
	for range workers {
		wg.Go(func() {
			// Nothing here should panic, but a worker that does must not take
			// the node down: its unfinished items stay unverified and the
			// sequential path redoes them.
			defer func() { _ = recover() }()
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
