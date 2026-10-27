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
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	apperr "github.com/celestiaorg/celestia-app/v11/app/errors"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
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
	handler := app.newAnteHandler(app.GetTxConfig().SignModeHandler())
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
		zkProofMsgCount int
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

	// Decode blob txs and pick out PFF decode candidates up front. Oversize txs are
	// left to the walk, which rejects them without decoding.
	var decodeCandidates []int
	for idx, rawTx := range req.Txs {
		if len(rawTx) > appconsts.MaxTxSize {
			continue
		}
		if cached == nil {
			decodedBlob, isBlobTx, err := blobtx.UnmarshalBlobTxNoCopy(rawTx)
			if isBlobTx {
				if err != nil {
					logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with blob tx %d", idx), err)
					return reject(), nil
				}
				blobTxs[idx] = decodedBlob
			}
		}
		if blobTxs[idx] != nil || !pffDecodeCandidate(rawTx) {
			continue
		}

		// Reject an over-full proposal before decoding. Same bound: pffDecodeCandidate also
		// counts malformed PayForFibre txs, which the loop rejects anyway.
		pffTxCount++
		if maxPFF > 0 && pffTxCount > maxPFF {
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max PayForFibre message count of %d", maxPFF))
			return reject(), nil
		}
		decodeCandidates = append(decodeCandidates, idx)
	}
	sdkTxs := make([]sdk.Tx, len(req.Txs))
	decoder := app.encodingConfig.TxConfig.TxDecoder()
	waitDecode := forEachParallel(len(decodeCandidates), func(candidate int) {
		idx := decodeCandidates[candidate]
		sdkTx, err := decoder(req.Txs[idx])
		if err != nil {
			return
		}
		sdkTxs[idx] = sdkTx
		if msg := fibretypes.PayForFibreMsg(sdkTx); msg != nil {
			decoded[idx] = fibrekeeper.DerivePayForFibre(req.Txs[idx], msg)
		}
		// Prime the SDK wrapper's lazy signer cache while each tx has one worker; ante still
		// reports any signer error or panic in block order.
		defer func() { _ = recover() }()
		if sigTx, ok := sdkTx.(authsigning.SigVerifiableTx); ok {
			_, _ = sigTx.GetSigners()
		}
	})

	waitDecode()

	// Warm the signature cache across every CPU alongside the sequential loop, once per
	// distinct certificate; the loop still decides.
	verifyPFF := app.FibreKeeper.PreparePreverification(ctx, decoded, fibrekeeper.PreverifyOptions{
		Certificates:       true,
		StopOnFirstFailure: true,
	})
	ante.PreverifyTxSignatures(ctx, app.AccountKeeper, app.GetTxConfig().SignModeHandler(), app.sigCache, sdkTxs)
	if verifyPFF != nil {
		// Runs alongside the walk; a panic is re-raised after it, so the recover above rejects the block.
		waitVerify := goWait(verifyPFF)
		defer waitVerify()
	}

	// One holder for the whole walk, so the context does not grow by a value
	// per transaction.
	certKeys := &fibretypes.CertKeyHolder{}
	ctx = fibretypes.WithCertKeyHolder(ctx, certKeys)

	// The data root depends only on the tx bytes and the decodes above, so build it
	// during the walk; a cache hit already has it.
	var (
		square       proposalSquareResult
		squareCancel atomic.Bool
		waitSquare   func()
	)
	if cached == nil {
		waitSquare = goWait(func() {
			square = app.buildProposalSquare(req.Txs, blobTxs, decoded, maxSquareSize, req.SquareSize, &squareCancel)
		})
		defer func() {
			// Cancel before waiting, so a proposal rejected at its first
			// tx does not pay for a whole square first.
			squareCancel.Store(true)
			waitSquare()
		}()
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
		sdkTx := sdkTxs[idx]
		var err error
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
				zkProofMsgCount += countZKProofMsgs(ctx, app.IBCKeeper.ChannelKeeper, msgs)
				if zkProofMsgCount > appconsts.MaxZKProofMessages {
					logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max ZK proof message count of %d", appconsts.MaxZKProofMessages))
					return reject(), nil
				}
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

	waitSquare()
	if square.err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failed to build proposal square", square.err)
		return reject(), nil
	}

	// Assert that the square size stated by the proposer is correct.
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

// goWait runs fn on its own goroutine; wait blocks until fn returns and re-raises its panic.
func goWait(fn func()) (wait func()) {
	return forEachParallel(1, func(int) { fn() })
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
