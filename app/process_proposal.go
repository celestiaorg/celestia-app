package app

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app/ante"
	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

const rejectedPropBlockLog = "Rejected proposal block:"

func (app *App) ProcessProposalHandler(ctx sdk.Context, req *abci.RequestProcessProposal) (resp *abci.ResponseProcessProposal, err error) {
	defer telemetry.MeasureSince(time.Now(), "process_proposal")
	// In the case of a panic resulting from an unexpected condition, it is
	// better for the liveness of the network to catch it, log an error, and
	// vote nil rather than crashing the node.
	defer func() {
		if err := recover(); err != nil {
			logInvalidPropBlock(app.Logger(), ctx.BlockHeader(), fmt.Sprintf("caught panic: %v", err))
			telemetry.IncrCounter(1, "process_proposal", "panics")
			resp = reject()
		}
	}()

	// Create the anteHandler that is used to check the validity of
	// transactions. All transactions need to be equally validated here
	// so that the nonce number is always correctly incremented (which
	// may affect the validity of future transactions).
	handler := app.newAnteHandler(app.proposalAccountKeeper(), app.GetTxConfig().SignModeHandler())
	blockHeader := ctx.BlockHeader()

	// Read the max square size before the ante loop. The loop reassigns ctx to
	// the context returned by the ante handler, which carries a finite gas meter
	// scoped to the last transaction. Reading it after the loop would meter this
	// block level read against that leftover meter and can run out of gas. This
	// mirrors PrepareProposal, which reads it before running any ante handler.
	maxSquareSize := app.MaxEffectiveSquareSize(ctx)

	// Reuse what PrepareProposal built for this height and these txs. A hit
	// skips the square, erasure coding and DAH; every per-tx check still runs.
	cached := app.proposalCache.Take(newProposalKey(ctx, blockHeader.Height, maxSquareSize), req.Txs)

	// Run the fibre BeginBlocker on the proposal branch, mirroring FinalizeBlock,
	// which pays out matured withdrawals and advances the freshness floor before
	// any tx. Pay-for-fibre settlement below must see that escrow state. The
	// branch is discarded, so nothing commits.
	if err := app.FibreKeeper.BeginBlocker(ctx); err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failed to run fibre begin blocker on proposal branch", err)
		return reject(), nil
	}

	var (
		sdkMessageCount int
		pfbMessageCount int
		pffMessageCount int
		pffTxCount      int
		maxPFF          = appconsts.MaxPayForFibreMessages
		// Decode caches for this proposal, index aligned with req.Txs: the
		// blob tx or pay-for-fibre tx decoded from req.Txs[i], nil if neither.
		// On a cache hit the proposer's own decodes are reused instead.
		blobTxs = make([]*blobtx.BlobTx, len(req.Txs))
		decoded = make([]*fibretypes.DecodedPayForFibre, len(req.Txs))
	)
	if cached != nil {
		blobTxs = cached.blobTxs
	}

	// Decode every tx once, up front. Oversize txs are left to the loop, which
	// rejects them without decoding.
	for idx, rawTx := range req.Txs {
		if len(rawTx) > appconsts.MaxTxSize {
			continue
		}
		if cached == nil {
			decodedBlob, isBlobTx, err := blobtx.UnmarshalBlobTx(rawTx)
			if isBlobTx {
				if err != nil {
					logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with blob tx %d", idx), err)
					return reject(), nil
				}
				blobTxs[idx] = decodedBlob
			}
		}
	}
	// Pick the pay-for-fibre txs before scheduling bounded SDK decoding.
	// Certificate inputs are then derived from that same decoded message.
	var decodeCandidates []int
	for idx, rawTx := range req.Txs {
		if len(rawTx) > appconsts.MaxTxSize || blobTxs[idx] != nil || !fibretypes.IsPayForFibreTx(rawTx) {
			continue
		}

		// Reject an over-full proposal here rather than after decoding every
		// tx. Same verdict and same bound: IsPayForFibreTx accepts a tx only
		// when it carries exactly one MsgPayForFibre, which is what the loop
		// counts.
		pffTxCount++
		if maxPFF > 0 && pffTxCount > maxPFF {
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max PayForFibre message count of %d", maxPFF))
			return reject(), nil
		}
		decodeCandidates = append(decodeCandidates, idx)
	}
	sdkTxs := make([]sdk.Tx, len(req.Txs))
	decodeErrs := make([]error, len(req.Txs))
	decoder := app.encodingConfig.TxConfig.TxDecoder()
	waitDecode := forEachParallel(len(decodeCandidates), func(candidate int) {
		idx := decodeCandidates[candidate]
		sdkTxs[idx], decodeErrs[idx] = decoder(req.Txs[idx])
		if decodeErrs[idx] != nil {
			return
		}
		if msg := fibretypes.PayForFibreMsg(sdkTxs[idx]); msg != nil {
			decoded[idx] = fibrekeeper.DerivePayForFibre(req.Txs[idx], msg)
		}
		// The SDK wrapper computes signers lazily. Prime its byte-bound cache
		// here while each tx has one worker; ante still reports any signer
		// error, or panic, in block order.
		defer func() { _ = recover() }()
		if sigTx, ok := sdkTxs[idx].(authsigning.SigVerifiableTx); ok {
			_, _ = sigTx.GetSigners()
		}
	})

	waitDecode()

	// Verify the block's pay-for-fibre signatures across every CPU alongside the
	// sequential loop. This only warms the cache; the loop still decides. Any
	// failure rejects the block, so the pass stops at the first one.
	// A block may repeat one certificate across many PayForFibre txs; the pass
	// holds that to one verification per distinct certificate.
	verifyPFF := app.FibreKeeper.PreparePreverification(ctx, decoded, fibrekeeper.PreverifyOptions{
		Certificates:       true,
		StopOnFirstFailure: true,
	})
	waitDecode()
	// Gated on there being candidates at all, not on the fibre pass preparing
	// work: it prepares nothing when every certificate is already cached, but
	// the tx signatures still are not.
	if verifyPFF != nil || len(decodeCandidates) > 0 {
		ante.PreverifyTxSignatures(ctx, app.AccountKeeper, app.GetTxConfig().SignModeHandler(), app.sigCache, sdkTxs)
		if verifyPFF != nil {
			var verificationWG sync.WaitGroup
			verificationWG.Go(func() {
				defer func() { _ = recover() }()
				verifyPFF()
			})
			defer verificationWG.Wait()
		}
	}

	// One holder for the whole walk, so the context does not grow by a value
	// per transaction.
	certKeys := &fibretypes.CertKeyHolder{}
	ctx = fibretypes.WithCertKeyHolder(ctx, certKeys)

	// The data root depends only on immutable tx bytes, so build it during the walk;
	// duplicate and malformed txs take the sequential path, starting no extra work.
	var (
		square       proposalSquareResult
		squareDone   chan struct{}
		squareCancel atomic.Bool
	)
	// Only an all-PayForFibre block (counted, and capped at maxPFF, by the decode loop)
	// qualifies, so a mixed block allocates nothing here.
	if cached == nil && pffTxCount == len(req.Txs) && len(req.Txs) >= minParallelSquareTxs {
		uniquePromises := make(map[sigcache.Key]struct{}, len(req.Txs))
		parallel := true
		for i, d := range decoded {
			if d == nil || !d.CertKeyed || !d.PromiseKeyed() || sdkTxs[i] == nil || decodeErrs[i] != nil {
				parallel = false
				break
			}
			// The promise key: a promise repeated under different signature sets has distinct
			// certificate keys, and the walk rejects it anyway.
			if _, seen := uniquePromises[d.PromiseKey]; seen {
				parallel = false
				break
			}
			uniquePromises[d.PromiseKey] = struct{}{}
		}
		if parallel {
			squareDone = make(chan struct{})
			go func() {
				defer close(squareDone)
				square = app.buildProposalSquare(req.Txs, blobTxs, decoded, maxSquareSize, req.SquareSize, &squareCancel)
			}()
			defer func() {
				// Cancel before waiting, so a proposal rejected at its first
				// tx does not pay for a whole square first.
				squareCancel.Store(true)
				<-squareDone
			}()
		}
	}

	// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed, non
	// blobTxs have no PFBs present and all txs are less than or equal to the max tx size limit
	for idx, rawTx := range req.Txs {
		sdkTxBytes := rawTx

		// all txs must be less than or equal to the max tx size limit
		currentTxSize := len(rawTx)
		if currentTxSize > appconsts.MaxTxSize {
			logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with tx %d", idx), errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes", currentTxSize, appconsts.MaxTxSize))
			return reject(), nil
		}

		// BlobTx is the most common special type; check it first. It was
		// decoded above, or by PrepareProposal from these exact bytes.
		blobTx := blobTxs[idx]
		isBlobTx := blobTx != nil
		if isBlobTx {
			sdkTxBytes = blobTx.Tx
		}

		// Pay-for-fibre txs were decoded above; every other tx is decoded here.
		sdkTx, err := sdkTxs[idx], decodeErrs[idx]
		if decoded[idx] == nil {
			sdkTx, err = decoder(sdkTxBytes)
		}
		ctx = ctx.WithTxBytes(sdkTxBytes)
		// Hand the ante handler the certificate key already derived from this
		// tx; a nil view clears the previous tx's key.
		certKeys.Set(decoded[idx])

		if err != nil {
			// An error here means that a tx was included in the block that is not decodable.
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d is not decodable", idx))
			return reject(), nil
		}

		// Handle non-blob transactions. This also validates MsgPayForFibre txs
		// (plain SDK txs, not wrapped in BlobTx).
		if !isBlobTx {
			msgs := sdkTx.GetMsgs()

			_, has := hasPFB(msgs)
			if has {
				// A non-blob tx has a PFB, which is invalid
				logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d has PFB but is not a blob tx", idx))
				return reject(), nil
			}

			// Validate MsgPayForFibre constraints.
			if err := validatePayForFibreTxShape(sdkTx); err != nil {
				logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d: %s", idx, err))
				return reject(), nil
			}

			isPFF := fibretypes.PayForFibreMsg(sdkTx) != nil
			if isPFF {
				pffMessageCount++
				if maxPFF > 0 && pffMessageCount > maxPFF {
					logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max PayForFibre message count of %d", maxPFF))
					return reject(), nil
				}
			} else {
				sdkMessageCount += countExecutableMsgs(ctx, app.IBCKeeper.ChannelKeeper, msgs)
				if sdkMessageCount > appconsts.MaxSDKMessages {
					logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max SDK message count of %d", appconsts.MaxSDKMessages))
					return reject(), nil
				}
			}

			// we need to increment the sequence for every transaction so that
			// the signature check below is accurate. this error only gets hit
			// if the account in question doesn't exist.
			ctx, err = handler(ctx, sdkTx, false)
			if err != nil {
				logInvalidPropBlockError(app.Logger(), blockHeader, "failure to increment sequence", err)
				return reject(), nil
			}

			// Settle after ante so later promises see the updated state and the tx pays
			// FinalizeBlock's gas. This ante pass is the only consensus check of PFF validator
			// signatures. No per-tx branch: any failure rejects the whole proposal.
			if isPFF {
				if execErr := runMsgs(ctx, msgs, app.MsgServiceRouter()); execErr != nil {
					logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("fibre settlement failed %d", idx), execErr)
					return reject(), nil
				}
			} else if containsFibreStateMsg(sdkTx) {
				// Replay fibre escrow effects in block order so later settlement
				// sees the FinalizeBlock balance. A failed message keeps the tx
				// (gas only), so don't reject.
				if execErr := executeTxMsgs(ctx, sdkTx, app.MsgServiceRouter()); execErr != nil {
					app.Logger().Debug("fibre state msg did not settle in proposal; keeping tx", "idx", idx, "err", execErr)
				}
			}

			// The non-blob path is complete; blob-specific checks below do not apply.
			continue
		}

		// validate the blobTx. This is the same validation used in CheckTx ensuring
		// - there is one PFB
		// - that each blob has a valid namespace
		// - that the sizes match
		// - that the namespaces match between blob and PFB
		// - that the share commitment is correct
		// If this tx was cached from CheckTx, we can skip the expensive
		// commitment verification since it was already validated. Otherwise, fall back to full validation.
		if _, err := app.ValidateBlobTxWithCache(blobTx); err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("blob tx validation failed %d", idx), err)
			return reject(), nil
		}

		pfbMessageCount += len(sdkTx.GetMsgs())
		if pfbMessageCount > appconsts.MaxPFBMessages {
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max PFB message count of %d", appconsts.MaxPFBMessages))
			return reject(), nil
		}

		ctx, err = handler(ctx, sdkTx, false)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, "ante handler validation failed", err)
			return reject(), nil
		}

	}

	// Compare the proposer's claims against the cached values.
	if cached != nil {
		if cached.squareSize != req.SquareSize {
			logInvalidPropBlock(app.Logger(), blockHeader, "proposed square size differs from calculated square size")
			return reject(), nil
		}
		if !bytes.Equal(cached.dataRoot, req.DataRootHash) {
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("proposed data root %X differs from calculated data root %X", req.DataRootHash, cached.dataRoot))
			return reject(), nil
		}
		return accept(), nil
	}

	if squareDone == nil {
		square = app.buildProposalSquare(req.Txs, blobTxs, decoded, maxSquareSize, req.SquareSize, &squareCancel)
	} else {
		<-squareDone
	}
	if square.err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failed to build proposal square", square.err)
		return reject(), nil
	}

	// Assert that the square size stated by the proposer is correct. Compare
	// the halved EDS width rather than the doubled proposer value: doubling an
	// attacker controlled uint64 wraps, so SquareSize and SquareSize+2^63 would
	// otherwise be indistinguishable.
	if square.size != req.SquareSize {
		logInvalidPropBlock(app.Logger(), blockHeader, "proposed square size differs from calculated square size")
		return reject(), nil
	}

	// by comparing the hashes we know the computed IndexWrappers (with the share indexes of the PFB's blobs)
	// are identical and that square layout is consistent. This also means that the share commitment rules
	// have been followed and thus each blobs share commitment should be valid
	if !bytes.Equal(square.root, req.DataRootHash) {
		logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("proposed data root %X differs from calculated data root %X", req.DataRootHash, square.root))
		return reject(), nil
	}

	return accept(), nil
}

// forEachParallel calls fn(i) for every i in [0, n) on up to GOMAXPROCS goroutines;
// the returned wait re-raises a panic from fn on the caller's goroutine.
func forEachParallel(n int, fn func(i int)) (wait func()) {
	var (
		next     atomic.Int64
		panicked atomic.Pointer[any]
		wg       sync.WaitGroup
	)
	for range min(runtime.GOMAXPROCS(0), n) {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					panicked.CompareAndSwap(nil, &r)
				}
			}()
			for i := int(next.Add(1)) - 1; i < n; i = int(next.Add(1)) - 1 {
				fn(i)
			}
		})
	}
	return func() {
		wg.Wait()
		if r := panicked.Load(); r != nil {
			panic(*r)
		}
	}
}

func hasPFB(msgs []sdk.Msg) (*blobtypes.MsgPayForBlobs, bool) {
	for _, msg := range msgs {
		if pfb, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
			return pfb, true
		}
	}
	return nil, false
}

func logInvalidPropBlock(l log.Logger, h tmproto.Header, reason string) {
	l.Error(
		rejectedPropBlockLog,
		"reason",
		reason,
		"proposer",
		h.ProposerAddress,
	)
}

func logInvalidPropBlockError(l log.Logger, h tmproto.Header, reason string, err error) {
	l.Error(
		rejectedPropBlockLog,
		"reason",
		reason,
		"proposer",
		h.ProposerAddress,
		"err",
		err.Error(),
	)
}

func reject() *abci.ResponseProcessProposal {
	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_REJECT,
	}
}

func accept() *abci.ResponseProcessProposal {
	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_ACCEPT,
	}
}

// ValidateBlobTxWithCache validates a blob transaction, using cached validation results when possible.
// It returns (fromCache, error) where fromCache indicates if the validation was skipped using cache.
func (app *App) ValidateBlobTxWithCache(blobTx *blobtx.BlobTx) (bool, error) {
	exists := app.txCache.Exists(blobTx.Tx, blobTx.Blobs)
	if exists {
		if _, err := blobtypes.ValidateBlobTxSkipCommitment(app.encodingConfig.TxConfig, blobTx); err != nil {
			return true, err
		}
		return true, nil
	}

	if err := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, blobTx, appconsts.SubtreeRootThreshold, appconsts.Version); err != nil {
		return false, err
	}
	return false, nil
}
