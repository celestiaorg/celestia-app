package app

import (
	"bytes"
	"runtime"
	"sync"
	"sync/atomic"

	storetypes "cosmossdk.io/store/types"
	appante "github.com/celestiaorg/celestia-app/v10/app/ante"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

// startPFFPreverification decodes candidate PFFs before returning. wait(i)
// joins the worker responsible for transaction i; finish joins all workers.
// Callers must finish before returning so no worker outlives the proposal.
func (app *App) startPFFPreverification(ctx sdk.Context, txs [][]byte) (decoded []sdk.Tx, wait func(int), finish func()) {
	const maxPreverified = 6000
	decoded = make([]sdk.Tx, min(len(txs), maxPreverified))
	done := make([]chan struct{}, len(decoded))
	wait = func(i int) {
		if i < len(done) && done[i] != nil {
			<-done[i]
		}
	}
	finish = func() {}
	limit := min(len(decoded), appconsts.MaxPayForFibreMessages)
	if limit == 0 {
		return decoded, wait, finish
	}

	type job struct {
		msg            *fibretypes.MsgPayForFibre
		key            fibreante.PffSigCacheKey
		tx             sdk.Tx
		rawTx          []byte
		done           chan struct{}
		preverifyTxSig bool
	}
	jobs := make([]job, 0, min(limit, 256))
	seen := make(map[fibreante.PffSigCacheKey]struct{}, min(limit, 256))
	type candidate struct {
		tx     sdk.Tx
		msg    *fibretypes.MsgPayForFibre
		cached bool
	}
	candidates := make([]candidate, len(decoded))
	decodeTx := app.encodingConfig.TxConfig.TxDecoder()
	decodeCandidate := func(i int) {
		// A malformed candidate must not crash a decoder worker. The ordered
		// proposal path still decodes and rejects it in the usual way.
		defer func() { _ = recover() }()
		rawTx := txs[i]
		if len(rawTx) > appconsts.MaxTxSize || !bytes.Contains(rawTx, []byte("MsgPayForFibre")) {
			return
		}
		if cachedTx, found := app.txCache.PFFTx(rawTx); found {
			candidates[i] = candidate{tx: cachedTx, cached: true}
			return
		}
		tx, err := decodeTx(rawTx)
		if err != nil {
			return
		}
		msg, ok := payForFibreMsg(tx)
		if ok {
			candidates[i] = candidate{tx: tx, msg: msg}
		}
	}
	parallelDecode := len(decoded) >= 128 && len(decoded) <= limit && runtime.GOMAXPROCS(0) > 1
	if parallelDecode {
		workers := min(max(1, runtime.NumCPU()/2), len(decoded))
		chunk := (len(decoded) + workers - 1) / workers
		var decodeWG sync.WaitGroup
		for start := 0; start < len(decoded); start += chunk {
			end := min(start+chunk, len(decoded))
			decodeWG.Go(func() {
				for i := start; i < end; i++ {
					decodeCandidate(i)
				}
			})
		}
		decodeWG.Wait()
	}
	covered := 0
	for i, rawTx := range txs[:len(decoded)] {
		if covered >= limit {
			break
		}
		if !parallelDecode {
			decodeCandidate(i)
		}
		item := candidates[i]
		if item.cached {
			decoded[i] = item.tx
			continue
		}
		if item.msg == nil {
			continue
		}
		covered++
		decoded[i] = item.tx
		feeTx, ok := item.tx.(sdk.FeeTx)
		if !ok || feeTx.GetGas() < fibretypes.EstimateGasForPayForFibreSignatureVerification(uint64(len(item.msg.ValidatorSignatures))) {
			continue
		}
		key, err := fibreante.NewPffSigCacheKey(item.msg)
		if err != nil || app.pffSigCache.IsCached(key) {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		done[i] = make(chan struct{})
		jobs = append(jobs, job{msg: item.msg, key: key, tx: item.tx, rawTx: rawTx, done: done[i], preverifyTxSig: hasCacheableDirectSignature(item.tx)})
	}
	if len(jobs) == 0 {
		return decoded, wait, finish
	}
	ctx = fibrekeeper.WithPreverifyValsetCache(ctx)
	// A successful direct-mode tx signature can also be reused by the normal
	// ante pass, which still checks account state and sequence in block order.
	signatureHandler := sdk.ChainAnteDecorators(
		sdkante.NewSetPubKeyDecorator(app.AccountKeeper),
		appante.NewCachedSigVerificationDecorator(app.AccountKeeper, app.GetTxConfig().SignModeHandler(), app.txSigCache),
	)

	workers := min(max(1, runtime.NumCPU()/2), len(jobs))
	work := make(chan job, len(jobs))
	var wg sync.WaitGroup
	var aborted atomic.Bool
	for range workers {
		// Each worker has an independent store cache and gas meter. It only reads
		// committed state; no worker writes its branch into the parent context.
		workerCtx, _ := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).CacheContext()
		wg.Go(func() {
			for item := range work {
				func() {
					defer func() {
						// A malformed candidate must never strand the ordered ante pass.
						_ = recover()
						close(item.done)
					}()
					if aborted.Load() {
						return
					}
					valid := func() (ok bool) {
						// A malformed item must not crash a worker or strand the sender.
						defer func() { _ = recover() }()
						return app.FibreKeeper.ValidatePayForFibreSignatures(workerCtx, item.msg) == nil
					}()
					if valid {
						app.pffSigCache.Cache(item.key)
						// The worker's cache context isolates SetPubKey writes. Failure
						// leaves the sequential ante pass to reject the transaction.
						if item.preverifyTxSig {
							func() {
								defer func() { _ = recover() }()
								_, _ = signatureHandler(workerCtx.WithTxBytes(item.rawTx).WithEventManager(sdk.NewEventManager()), item.tx, false)
							}()
						}
					} else {
						aborted.Store(true)
					}
				}()
			}
		})
	}
	for _, item := range jobs {
		work <- item
	}
	close(work)
	finish = wg.Wait
	return decoded, wait, finish
}

func hasCacheableDirectSignature(tx sdk.Tx) bool {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return false
	}
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 {
		return false
	}
	single, ok := sigs[0].Data.(*signing.SingleSignatureData)
	return ok && single.SignMode == signing.SignMode_SIGN_MODE_DIRECT
}
