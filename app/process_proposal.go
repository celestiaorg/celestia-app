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
	squarev4 "github.com/celestiaorg/go-square/v4"
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

	// Reuse the artifacts this node produced in PrepareProposal when this is the
	// block it proposed. The entry is keyed on the exact tx bytes, so a hit can
	// only describe this block, but a hit is still not evidence of validity:
	// every check below runs unchanged, and the square size and data root the
	// proposer stated are still compared against what the cached artifacts
	// imply. A node that misses recomputes everything and reaches the same
	// result.
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
		maxPFF          = appconsts.MaxPayForFibreMessages
		// blobTxs and decoded are the decode caches for this proposal: the blob
		// tx decoded from req.Txs[i], or the pay-for-fibre tx decoded from it,
		// or nil where that tx is neither. They are scoped to this call, so an
		// entry can only ever describe the bytes it came from. On a cache hit
		// the cached blob txs, bound to the same bytes, are used instead.
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
	// Each pay-for-fibre decode depends only on its transaction bytes. The blob
	// scan above still reports malformed blob transactions in their original
	// order before this speculative work starts.
	var fibreNext atomic.Int64
	var fibreDecodeWG sync.WaitGroup
	var fibreDecodePanicked atomic.Bool
	for range min(runtime.NumCPU(), len(req.Txs)) {
		fibreDecodeWG.Go(func() {
			for {
				idx := int(fibreNext.Add(1)) - 1
				if idx >= len(req.Txs) {
					return
				}
				if len(req.Txs[idx]) > appconsts.MaxTxSize || blobTxs[idx] != nil {
					continue
				}
				func() {
					defer func() {
						if recover() != nil {
							fibreDecodePanicked.Store(true)
						}
					}()
					decoded[idx] = fibrekeeper.DecodePayForFibre(req.Txs[idx])
				}()
			}
		})
	}
	fibreDecodeWG.Wait()
	if fibreDecodePanicked.Load() {
		logInvalidPropBlock(app.Logger(), blockHeader, "caught panic while decoding PayForFibre tx")
		return reject(), nil
	}

	// SDK decoding is independent per transaction. Prefetch only parseable
	// PayForFibre txs, up to the versioned block limit: a rejected block must
	// not make us decode an unbounded number of otherwise skipped SDK txs.
	// Consume the results in block order below.
	decodeCandidates := make([]int, 0, min(len(req.Txs), max(0, maxPFF)))
	for idx, d := range decoded {
		if d != nil && (maxPFF <= 0 || len(decodeCandidates) < maxPFF) {
			decodeCandidates = append(decodeCandidates, idx)
		}
	}
	sdkTxs := make([]sdk.Tx, len(req.Txs))
	decodeErrs := make([]error, len(req.Txs))
	decoder := app.encodingConfig.TxConfig.TxDecoder()
	var decodeNext atomic.Int64
	var decodeWG sync.WaitGroup
	for range min(runtime.NumCPU(), len(decodeCandidates)) {
		decodeWG.Go(func() {
			for {
				candidate := int(decodeNext.Add(1)) - 1
				if candidate >= len(decodeCandidates) {
					return
				}
				idx := decodeCandidates[candidate]
				rawTx := req.Txs[idx]
				func() {
					defer func() {
						if r := recover(); r != nil {
							decodeErrs[idx] = fmt.Errorf("transaction decoder panic: %v", r)
						}
					}()
					sdkTxs[idx], decodeErrs[idx] = decoder(rawTx)
					if decodeErrs[idx] == nil {
						// The SDK wrapper computes signers lazily. Prime its
						// byte-bound cache here while each tx has one worker;
						// ante still reports any signer error in block order.
						func() {
							defer func() { _ = recover() }()
							if sigTx, ok := sdkTxs[idx].(authsigning.SigVerifiableTx); ok {
								_, _ = sigTx.GetSigners()
							}
						}()
					}
				}()
			}
		})
	}

	// Verify the block's pay-for-fibre signatures across every CPU before the
	// sequential loop reaches them. This only warms the signature cache: a
	// failed check is not recorded, so the loop below still performs it and
	// still decides. Any failure rejects the whole block, so the pass stops
	// claiming work at the first one.
	app.FibreKeeper.PreverifyDecoded(ctx, decoded, fibrekeeper.PreverifyOptions{
		Certificates:       true,
		StopOnFirstFailure: true,
	})
	decodeWG.Wait()
	// Gated on there being PayForFibre candidates at all, not on whether the
	// fibre pass queued work: it queues nothing when every promise and
	// certificate is already cached, and the transaction signatures still are
	// not.
	if len(decodeCandidates) > 0 {
		ante.NewCachedSigVerificationDecorator(app.AccountKeeper, app.GetTxConfig().SignModeHandler(), app.sigCache).
			PreverifyTxSignatures(ctx, sdkTxs)
	}

	// One holder for the whole walk: the decoded view of the transaction being
	// processed is handed over by setting its field, not by attaching a new
	// context value per transaction, which would grow the context chain by one
	// node per transaction.
	decodedHolder := &fibretypes.DecodedHolder{}
	ctx = fibretypes.WithDecodedHolder(ctx, decodedHolder)

	// For a large block of distinct, decoded PFF txs, the data root depends
	// only on immutable tx bytes. Build it while the ordered ante and settlement
	// walk runs. Duplicate transactions and malformed txs take the sequential
	// path so cheap rejected proposals do not start extra square work.
	var (
		squareDone   chan proposalSquareResult
		squareCancel atomic.Bool
	)
	if cached == nil && len(req.Txs) >= 100 && (maxPFF <= 0 || len(req.Txs) <= maxPFF) {
		uniqueCerts := make(map[sigcache.Key]struct{}, len(req.Txs))
		uniquePromises := make(map[sigcache.Key]struct{}, len(req.Txs))
		parallel := true
		for i, d := range decoded {
			if d == nil || !d.CertKeyed || !d.PromiseKeyed || sdkTxs[i] == nil || decodeErrs[i] != nil {
				parallel = false
				break
			}
			// Both keys, not just the certificate: the walk rejects a repeated
			// promise on its hash, which covers the promise alone, so two txs
			// carrying one promise under different signature sets have
			// distinct certificate keys and would otherwise slip through to
			// the parallel path and be rejected anyway.
			_, certSeen := uniqueCerts[d.CertKey]
			_, promiseSeen := uniquePromises[d.PromiseKey]
			if certSeen || promiseSeen {
				parallel = false
				break
			}
			uniqueCerts[d.CertKey] = struct{}{}
			uniquePromises[d.PromiseKey] = struct{}{}
		}
		if parallel {
			squareDone = make(chan proposalSquareResult, 1)
			go func(done chan<- proposalSquareResult) {
				// The send must happen on every path: a goroutine that died
				// without sending would leave the drain below blocking
				// forever, which is a silent node halt.
				result := proposalSquareResult{
					stage: "panic while building proposal square",
					err:   fmt.Errorf("proposal square builder did not finish"),
				}
				defer func() {
					_ = recover()
					done <- result
				}()
				result = app.buildProposalSquare(req.Txs, blobTxs, decoded, maxSquareSize, req.SquareSize, &squareCancel)
			}(squareDone)
			defer func() {
				if squareDone != nil {
					// Tell the builder to stop before waiting for it, so a
					// proposal rejected at its first transaction does not pay
					// for a whole square and extended square first.
					squareCancel.Store(true)
					<-squareDone
				}
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

		sdkTx, err := sdkTxs[idx], decodeErrs[idx]
		if sdkTx == nil && err == nil {
			sdkTx, err = decoder(sdkTxBytes)
		}
		ctx = ctx.WithTxBytes(sdkTxBytes)
		// Hand the ante handler and the message server what was already
		// decoded from this tx; nil clears the previous tx's value.
		decodedHolder.Set(decoded[idx])

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

			pffMsg, isPFF := payForFibreMsg(sdkTx)
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

			// Settle after ante so later promises see the updated state and the tx
			// pays the same gas it would in FinalizeBlock. This ante pass is also
			// the consensus enforcement point for PFF validator signatures:
			// FinalizeBlock never re-verifies them (see
			// FibreSignatureVerificationDecorator), so removing it would let a
			// proposer settle promises without a validator quorum.
			if isPFF {
				if execErr := executeProposalPFF(ctx, pffMsg, app.MsgServiceRouter()); execErr != nil {
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

	// On a hit the square, extended square and data availability header are a
	// pure function of the tx bytes, the max square size and the subtree root
	// threshold, all of which the cache key pins down, so PrepareProposal's
	// results are exactly what recomputing here would produce. The proposer's
	// stated square size and data root are still compared against them.
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

	var square proposalSquareResult
	if squareDone == nil {
		square = app.buildProposalSquare(req.Txs, blobTxs, decoded, maxSquareSize, req.SquareSize, &squareCancel)
	} else {
		square = <-squareDone
		squareDone = nil
	}
	if square.err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, square.stage, square.err)
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

// classifyTxs is fibretypes.ClassifyTxs with the blob and pay-for-fibre txs
// already decoded. blobTxs and decoded are index aligned with txs; a tx that is
// neither takes the parsing path, which also covers a pay-for-fibre tx the
// decoded view could not parse.
func classifyTxs(txs [][]byte, blobTxs []*blobtx.BlobTx, decoded []*fibretypes.DecodedPayForFibre) ([]squarev4.ClassifiedTx, error) {
	classified := make([]squarev4.ClassifiedTx, len(txs))
	for i, rawTx := range txs {
		var (
			fibreTx   *blobtx.FibreTx
			isFibreTx bool
			err       error
		)
		switch {
		case blobTxs[i] != nil:
			classified[i] = squarev4.NewClassifiedTx(rawTx)
			continue
		case decoded[i] != nil:
			isFibreTx = true
			fibreTx, err = decoded[i].FibreTx()
		default:
			fibreTx, isFibreTx, err = fibretypes.TryParseFibreTx(rawTx)
		}
		if err != nil {
			return nil, fmt.Errorf("parsing fibre tx at index %d: %w", i, err)
		}
		if !isFibreTx {
			classified[i] = squarev4.NewClassifiedTx(rawTx)
			continue
		}
		classified[i], err = squarev4.NewClassifiedFibreTx(fibreTx)
		if err != nil {
			return nil, fmt.Errorf("classifying fibre tx at index %d: %w", i, err)
		}
	}
	return classified, nil
}

// constructSquare is squarev4.Construct with the blob tx decoding taken out:
// that helper unmarshals every blob tx twice, once to check the ordering and
// once to append it, and each unmarshal copies every blob. The ordering rules,
// the append order and the resulting square are unchanged.
//
// blobTxs is index aligned with txs and non-nil exactly where the tx is a blob
// tx that decoded, which the caller has already established.
func constructSquare(txs []squarev4.ClassifiedTx, blobTxs []*blobtx.BlobTx, maxSquareSize, subtreeRootThreshold int) (squarev4.Square, error) {
	builder, err := squarev4.NewBuilder(maxSquareSize, subtreeRootThreshold)
	if err != nil {
		return nil, err
	}

	var seenBlobTx, seenFibreTx bool
	for idx, classified := range txs {
		if err := classified.Validate(); err != nil {
			return nil, fmt.Errorf("classified tx at index %d: %w", idx, err)
		}

		switch {
		case classified.FibreTx != nil:
			seenFibreTx = true
			added, err := builder.AppendFibreTx(classified.FibreTx)
			if err != nil {
				return nil, fmt.Errorf("appending fibre tx at index %d: %w", idx, err)
			}
			if !added {
				return nil, fmt.Errorf("not enough space to append fibre tx at index %d", idx)
			}

		case blobTxs[idx] != nil:
			if seenFibreTx {
				return nil, fmt.Errorf("blob tx at index %d cannot be appended after pay-for-fibre tx", idx)
			}
			seenBlobTx = true
			added, err := builder.AppendBlobTx(blobTxs[idx])
			if err != nil {
				return nil, fmt.Errorf("appending blob tx at index %d: %w", idx, err)
			}
			if !added {
				return nil, fmt.Errorf("not enough space to append blob tx at index %d", idx)
			}

		default:
			if seenBlobTx {
				return nil, fmt.Errorf("normal tx at index %d cannot be appended after blob tx", idx)
			}
			if seenFibreTx {
				return nil, fmt.Errorf("normal tx at index %d cannot be appended after pay-for-fibre tx", idx)
			}
			if !builder.AppendTx(classified.Bytes) {
				return nil, fmt.Errorf("not enough space to append tx at index %d", idx)
			}
		}
	}

	return builder.Export()
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
