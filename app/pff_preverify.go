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
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
)

// preverifyPFFSignatures warms the certificate cache before the sequential ante
// pass. A failed or skipped check remains the ante handler's responsibility.
func (app *App) preverifyPFFSignatures(ctx sdk.Context, txs [][]byte) []sdk.Tx {
	const maxPreverified = 6000
	decoded := make([]sdk.Tx, min(len(txs), maxPreverified))
	limit := min(len(decoded), appconsts.MaxPayForFibreMessages)
	if limit == 0 {
		return decoded
	}

	type job struct {
		msg   *fibretypes.MsgPayForFibre
		key   fibreante.PffSigCacheKey
		tx    sdk.Tx
		rawTx []byte
	}
	jobs := make([]job, 0, min(limit, 256))
	seen := make(map[fibreante.PffSigCacheKey]struct{}, min(limit, 256))
	covered := 0
	for i, rawTx := range txs[:len(decoded)] {
		if covered >= limit {
			break
		}
		if len(rawTx) > appconsts.MaxTxSize || !bytes.Contains(rawTx, []byte("MsgPayForFibre")) {
			continue
		}
		if app.txCache.Exists(rawTx, nil) {
			continue
		}
		tx, err := app.encodingConfig.TxConfig.TxDecoder()(rawTx)
		if err != nil {
			continue
		}
		msg, ok := payForFibreMsg(tx)
		if !ok {
			continue
		}
		covered++
		decoded[i] = tx
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok || feeTx.GetGas() < fibretypes.EstimateGasForPayForFibreSignatureVerification(uint64(len(msg.ValidatorSignatures))) {
			continue
		}
		key, err := fibreante.NewPffSigCacheKey(msg)
		if err != nil || app.pffSigCache.IsCached(key) {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		jobs = append(jobs, job{msg: msg, key: key, tx: tx, rawTx: rawTx})
	}
	if len(jobs) == 0 {
		return decoded
	}
	ctx = fibrekeeper.WithPreverifyValsetCache(ctx)
	// A successful direct-mode tx signature can also be reused by the normal
	// ante pass, which still checks account state and sequence in block order.
	signatureHandler := sdk.ChainAnteDecorators(
		sdkante.NewSetPubKeyDecorator(app.AccountKeeper),
		appante.NewCachedSigVerificationDecorator(app.AccountKeeper, app.GetTxConfig().SignModeHandler(), app.txSigCache),
	)

	workers := min(runtime.NumCPU(), len(jobs))
	work := make(chan job, workers)
	var wg sync.WaitGroup
	var aborted atomic.Bool
	for range workers {
		// Each worker has an independent store cache and gas meter. It only reads
		// committed state; no worker writes its branch into the parent context.
		workerCtx, _ := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).CacheContext()
		wg.Go(func() {
			for item := range work {
				if aborted.Load() {
					continue
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
					func() {
						defer func() { _ = recover() }()
						_, _ = signatureHandler(workerCtx.WithTxBytes(item.rawTx).WithEventManager(sdk.NewEventManager()), item.tx, false)
					}()
				} else {
					aborted.Store(true)
				}
			}
		})
	}
	for _, item := range jobs {
		work <- item
	}
	close(work)
	wg.Wait()
	return decoded
}
