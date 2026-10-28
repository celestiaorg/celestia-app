package app

import (
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/tx"
	tmbytes "github.com/cometbft/cometbft/libs/bytes"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FilteredSquareBuilder filters txs and blobs using a copy of the state and tx validity
// rules before adding it to the square.
type FilteredSquareBuilder struct {
	handler   sdk.AnteHandler
	msgRouter baseapp.MessageRouter
	txConfig  client.TxConfig
	chanKeep  channelKeeper
	builder   *square.Builder
	// validateBlobTx, when set, drops blob txs it returns an error for.
	validateBlobTx func(*tx.BlobTx) (bool, error)
	// keptBlobTxs is index aligned with the list Fill returned: the decoded
	// blob tx at each position, or nil where the tx is not a blob tx.
	keptBlobTxs []*tx.BlobTx
	// decodePFF decodes a pay-for-fibre tx once for the whole Fill;
	// preverifyPFF warms the signature cache. Both are optional.
	decodePFF    func([]byte) *fibretypes.DecodedPayForFibre
	preverifyPFF func(sdk.Context, []*fibretypes.DecodedPayForFibre)
}

func NewFilteredSquareBuilder(
	handler sdk.AnteHandler,
	msgRouter baseapp.MessageRouter,
	txConfig client.TxConfig,
	chanKeep channelKeeper,
	maxSquareSize,
	subtreeRootThreshold int,
) (*FilteredSquareBuilder, error) {
	builder, err := square.NewBuilder(maxSquareSize, subtreeRootThreshold)
	if err != nil {
		return nil, err
	}
	return &FilteredSquareBuilder{
		handler:   handler,
		msgRouter: msgRouter,
		txConfig:  txConfig,
		chanKeep:  chanKeep,
		builder:   builder,
	}, nil
}

func (fsb *FilteredSquareBuilder) Build() (square.Square, error) {
	return fsb.builder.Export()
}

func (fsb *FilteredSquareBuilder) Builder() *square.Builder {
	return fsb.builder
}

// KeptBlobTxs returns the decoded blob txs of the last Fill, index aligned with
// the list it returned and nil where a tx is not a blob tx.
func (fsb *FilteredSquareBuilder) KeptBlobTxs() []*tx.BlobTx {
	return fsb.keptBlobTxs
}

func (fsb *FilteredSquareBuilder) Fill(ctx sdk.Context, txs [][]byte, maxTxBytes int64) [][]byte {
	logger := ctx.Logger().With("app/filtered-square-builder")

	// Drop any txs whose cumulative on-wire size would exceed maxTxBytes.
	// Required because the data square can be configured larger than
	// block.MaxBytes.
	//
	// Size each tx by its protobuf-framed length (tag + length prefix + bytes),
	// matching how CometBFT validates the returned list. Raw len(tx) under-counts
	// the framing, so a list that fits here could still be rejected by Core. The
	// kept list is a subset of what's admitted here, so the bound carries over.
	//
	// A non-positive maxTxBytes means "no limit" — CometBFT always sets a
	// positive value in production, but some tests construct requests
	// directly and leave it at zero.
	filteredByMaxBytes := txs
	if maxTxBytes > 0 {
		var currentTxBytes int64
		filteredByMaxBytes = make([][]byte, 0, len(txs))
		for _, tx := range txs {
			txProtoSize := coretypes.ComputeProtoSizeForTxs([]coretypes.Tx{tx})
			if currentTxBytes+txProtoSize > maxTxBytes {
				logger.Debug("skipping tx because it was too large to fit in the block", "tx", lazyTxHash(tx))
				continue
			}
			currentTxBytes += txProtoSize
			filteredByMaxBytes = append(filteredByMaxBytes, tx)
		}
	}

	// note that there is an additional filter step for tx size of raw txs here
	normalTxs, rawBlobTxs, payForFibreTxs := separateTxsDecoded(logger, fsb.txConfig, filteredByMaxBytes)
	dec := fsb.txConfig.TxDecoder()

	// Warm the signature cache across every CPU before filling. Failures are
	// not recorded, so the fill still performs and decides each check itself.
	if fsb.preverifyPFF != nil {
		decoded := make([]*fibretypes.DecodedPayForFibre, 0, min(len(payForFibreTxs), appconsts.MaxPayForFibreCandidates))
		for i := range payForFibreTxs {
			if len(decoded) >= appconsts.MaxPayForFibreCandidates {
				break
			}
			if fsb.decodeFibreTx(logger, dec, &payForFibreTxs[i]) {
				decoded = append(decoded, payForFibreTxs[i].decoded)
			}
		}
		fsb.preverifyPFF(ctx, decoded)
	}

	var (
		sdkMessageCount = 0
		zkProofMsgCount = 0
		ismSigCount     = 0
		pfbMessageCount = 0
		n               = 0
		m               = 0
	)

	for _, tx := range normalTxs {
		// CheckTx rejects txs without messages, so every tx here weighs at least one message
		// and nothing more fits once the cap is reached.
		if sdkMessageCount >= appconsts.MaxSDKMessages {
			break
		}
		// separateTxsDecoded leaves these undecoded; drop what it drops on a full decode.
		sdkTx, err := dec(tx)
		if err != nil {
			continue
		}
		if _, has := hasPFB(sdkTx.GetMsgs()); has || countMsgPayForFibre(sdkTx) > 0 {
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(tx)

		execMsgCount := countExecutableMsgs(ctx, fsb.chanKeep, sdkTx.GetMsgs())
		if sdkMessageCount+execMsgCount > appconsts.MaxSDKMessages {
			logger.Debug("skipping tx because the max SDK message count was reached", "tx", lazyTxHash(tx))
			continue
		}
		zkProofCount := countZKProofMsgs(ctx, fsb.chanKeep, sdkTx.GetMsgs())
		if zkProofMsgCount+zkProofCount > appconsts.MaxZKProofMessages {
			logger.Debug("skipping tx because the max ZK proof message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
			continue
		}
		txISMSigCount := countISMSignatures(ctx, fsb.chanKeep, sdkTx.GetMsgs())
		if ismSigCount+txISMSigCount > appconsts.MaxISMSignatures {
			logger.Debug("skipping tx because the max ISM signature count was reached", "tx", lazyTxHash(tx))
			continue
		}

		if !fsb.builder.AppendTx(tx) {
			logger.Debug("skipping tx because it was too large to fit in the square", "tx", lazyTxHash(tx))
			continue
		}

		// Branch the state per tx so a rejected tx leaves no partial writes
		// behind for the txs that follow it.
		txCtx, commit := ctx.CacheContext()
		txCtx, err = fsb.handler(txCtx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked transaction",
				"tx", lazyTxHash(tx),
				"error", err,
				"msgs", msgTypes(sdkTx),
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_std_txs")
			err = fsb.builder.RevertLastTx()
			if err != nil {
				logger.Error("reverting last transaction", "error", err)
			}
			continue
		}
		commit()
		ctx = txCtx.WithMultiStore(ctx.MultiStore()).WithEventManager(ctx.EventManager())

		// Replay fibre escrow effects (e.g. a timeout debit) so later pay-for-fibre
		// settlement sees the same balance it will in FinalizeBlock. A failed
		// message keeps the tx (gas only), so ignore the error.
		if containsFibreStateMsg(sdkTx) {
			if err := executeTxMsgs(ctx, sdkTx, fsb.msgRouter); err != nil {
				logger.Debug("fibre state msg did not settle in proposal; keeping tx", "tx", lazyTxHash(tx), "error", err)
			}
		}

		sdkMessageCount += execMsgCount
		zkProofMsgCount += zkProofCount
		ismSigCount += txISMSigCount
		normalTxs[n] = tx
		n++
	}

	var keptBlobTxs []*tx.BlobTx
	for _, rawTx := range rawBlobTxs {
		// validateBlobTx keeps only single-PFB blob txs, so nothing more fits once the cap is reached.
		if pfbMessageCount >= appconsts.MaxPFBMessages {
			break
		}
		bTx, _, err := tx.UnmarshalBlobTxNoCopy(rawTx)
		if err != nil {
			// Drop malformed blob txs. Matches ProcessProposalHandler. CheckTx should have
			// rejected this; reaching here indicates a regression so log + count it.
			logger.Error("dropping malformed blob tx", "tx", lazyTxHash(rawTx), "err", err)
			telemetry.IncrCounter(1, "prepare_proposal", "malformed_blob_txs")
			continue
		}
		sdkTx, err := dec(bTx.Tx)
		if err != nil {
			logger.Error("decoding already checked blob transaction", "tx", lazyTxHash(bTx.Tx), "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(bTx.Tx)

		if pfbMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxPFBMessages {
			logger.Debug("skipping blob tx because the max pfb message count was reached", "tx", lazyTxHash(bTx.Tx))
			continue
		}

		if fsb.validateBlobTx != nil {
			if _, err := fsb.validateBlobTx(bTx); err != nil {
				logger.Error("dropping invalid blob tx", "tx", lazyTxHash(bTx.Tx), "error", err)
				continue
			}
		}

		ok, err := fsb.builder.AppendBlobTx(bTx)
		if err != nil {
			logger.Debug("skipping blob tx due to error", "tx", lazyTxHash(bTx.Tx), "err", err)
			continue
		}
		if !ok {
			logger.Debug("skipping tx because it was too large to fit in the square", "tx", lazyTxHash(bTx.Tx))
			continue
		}

		txCtx, commit := ctx.CacheContext()
		txCtx, err = fsb.handler(txCtx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked blob transaction", "tx", lazyTxHash(bTx.Tx), "error", err,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_blob_txs")
			err = fsb.builder.RevertLastBlobTx()
			if err != nil {
				logger.Error("reverting last blob transaction failed", "error", err)
			}
			continue
		}
		commit()
		ctx = txCtx.WithMultiStore(ctx.MultiStore()).WithEventManager(ctx.EventManager())

		pfbMessageCount += len(sdkTx.GetMsgs())
		rawBlobTxs[m] = rawTx
		keptBlobTxs = append(keptBlobTxs, bTx)
		m++
	}

	fibreTxs := processFibreTxsForSquare(fsb, ctx, dec, payForFibreTxs)

	kept := make([][]byte, 0, n+m+len(fibreTxs))
	kept = append(kept, normalTxs[:n]...)
	// UnmarshalBlobTx only accepts canonically encoded blob txs, so the raw bytes
	// are identical to re-marshaling the decoded blob txs and can be reused.
	kept = append(kept, rawBlobTxs[:m]...)
	kept = append(kept, fibreTxs...)

	// Record where the decoded blob txs ended up so ProcessProposal reuses
	// them. Only blob txs occupy this range; normal and fibre txs stay nil.
	fsb.keptBlobTxs = make([]*tx.BlobTx, len(kept))
	copy(fsb.keptBlobTxs[n:n+m], keptBlobTxs)
	return kept
}

func msgTypes(sdkTx sdk.Tx) []string {
	msgs := sdkTx.GetMsgs()
	msgNames := make([]string, len(msgs))
	for i, msg := range msgs {
		msgNames[i] = sdk.MsgTypeURL(msg)
	}
	return msgNames
}

// decodedFibreTx pairs a raw pay-for-fibre tx with its decoded forms, filled on first use.
type decodedFibreTx struct {
	raw     []byte
	sdkTx   sdk.Tx
	decoded *fibretypes.DecodedPayForFibre
	// checked is set once decodeFibreTx ran; valid is its result.
	checked, valid bool
}

// decodeFibreTx decodes e once and reports whether ProcessProposal accepts its shape.
// decodePFF and the shape check are stateless, so when this runs does not matter.
func (fsb *FilteredSquareBuilder) decodeFibreTx(logger log.Logger, dec sdk.TxDecoder, e *decodedFibreTx) bool {
	if e.checked {
		return e.valid
	}
	e.checked = true
	if e.sdkTx == nil {
		sdkTx, err := dec(e.raw)
		if err != nil {
			return false
		}
		e.sdkTx = sdkTx
	}
	// Reuse the predicate ProcessProposal enforces so a proposer never
	// builds a block its own ProcessProposal would reject.
	if err := validatePayForFibreTxShape(e.sdkTx); err != nil {
		logger.Debug("dropping invalid pay-for-fibre tx", "tx", lazyTxHash(e.raw), "err", err)
		return false
	}
	if fsb.decodePFF != nil {
		e.decoded = fsb.decodePFF(e.raw)
	}
	e.valid = true
	return true
}

// separateTxsDecoded separates raw tendermint txs into normal, blob, and
// pay-for-fibre txs.
// This function filters out:
//   - transactions that exceed MaxTxSize
//   - transactions that fail SDK decoding
//   - non-blob transactions containing MsgPayForBlobs
//
// Txs whose first message parses from the wire are not decoded here; Fill decodes and
// filters them when it reaches them (pay-for-fibre txs through decodeFibreTx). Blob txs
// are only classified by their type ID; Fill decodes them and drops malformed ones.
func separateTxsDecoded(logger log.Logger, txConfig client.TxConfig, rawTxs [][]byte) (normalTxs, rawBlobTxs [][]byte, payForFibreTxs []decodedFibreTx) {
	normalTxs = make([][]byte, 0, len(rawTxs))
	rawBlobTxs = make([][]byte, 0, len(rawTxs))
	payForFibreTxs = make([]decodedFibreTx, 0, len(rawTxs))
	dec := txConfig.TxDecoder()

	for _, rawTx := range rawTxs {
		// this check in theory shouldn't get hit, as txs should be filtered
		// in CheckTx. However in tests we're inserting too large of txs
		// therefore also filter here.
		if len(rawTx) > appconsts.MaxTxSize {
			continue
		}

		// The same predicate as UnmarshalBlobTx's isBlob, without decoding the blobs.
		if tx.HasBlobTxTypeID(rawTx) {
			rawBlobTxs = append(rawBlobTxs, rawTx)
			continue
		}

		// Fill decodes these lazily and stops at each cap; a failed peek takes the full decode.
		if isPFF, ok := firstMsgIsPayForFibre(rawTx); ok {
			if isPFF {
				payForFibreTxs = append(payForFibreTxs, decodedFibreTx{raw: rawTx})
			} else {
				normalTxs = append(normalTxs, rawTx)
			}
			continue
		}

		sdkTx, err := dec(rawTx)
		if err != nil {
			// Skip txs that fail decoding. ProcessProposal rejects
			// undecodable txs, so there is no reason to include them.
			continue
		}

		if _, has := hasPFB(sdkTx.GetMsgs()); has {
			// ProcessProposal rejects non-blob txs that contain a PFB.
			continue
		}

		if countMsgPayForFibre(sdkTx) > 0 {
			payForFibreTxs = append(payForFibreTxs, decodedFibreTx{raw: rawTx, sdkTx: sdkTx})
			continue
		}

		normalTxs = append(normalTxs, rawTx)
	}
	return normalTxs, rawBlobTxs, payForFibreTxs
}

var payForFibreTypeURL = sdk.MsgTypeURL(&fibretypes.MsgPayForFibre{})

// firstMsgIsPayForFibre reports whether the first message of rawTx is a MsgPayForFibre, decoding
// only the body and the type URLs. ok is false if either does not unmarshal.
func firstMsgIsPayForFibre(rawTx []byte) (isPFF, ok bool) {
	var raw fibretypes.TxRawBody
	if err := raw.Unmarshal(rawTx); err != nil {
		return false, false
	}
	var body fibretypes.TxBodyTypeURLs
	if err := body.Unmarshal(raw.BodyBytes); err != nil {
		return false, false
	}
	return len(body.Messages) > 0 && body.Messages[0].TypeUrl == payForFibreTypeURL, true
}

// countMsgPayForFibre returns the number of MsgPayForFibre messages in a transaction.
func countMsgPayForFibre(sdkTx sdk.Tx) int {
	count := 0
	for _, msg := range sdkTx.GetMsgs() {
		if _, ok := msg.(*fibretypes.MsgPayForFibre); ok {
			count++
		}
	}
	return count
}

// processFibreTxsForSquare processes pay-for-fibre transactions: synthesize
// system blob, validate, append to builder. Returns the raw tx bytes of
// accepted fibre txs.
func processFibreTxsForSquare(fsb *FilteredSquareBuilder, ctx sdk.Context, dec sdk.TxDecoder, payForFibreTxs []decodedFibreTx) [][]byte {
	logger := ctx.Logger().With("app/filtered-square-builder")
	var pffMessageCount int
	fibreTxs := make([][]byte, 0, min(len(payForFibreTxs), appconsts.MaxPayForFibreMessages))
	// A promise settles once; a later copy would fail settlement, so skip it before the ante.
	settled := make(map[sigcache.Key]struct{})
	certKeys := &fibretypes.CertKeyHolder{}
	ctx = fibretypes.WithCertKeyHolder(ctx, certKeys)

	for i := range payForFibreTxs {
		// Every tx decodeFibreTx accepts weighs exactly one message, so nothing more fits.
		if pffMessageCount >= appconsts.MaxPayForFibreMessages {
			break
		}
		entry := &payForFibreTxs[i]
		if !fsb.decodeFibreTx(logger, dec, entry) {
			continue
		}
		rawTx, sdkTx := entry.raw, entry.sdkTx
		if entry.decoded != nil && entry.decoded.PromiseKeyed() {
			if _, ok := settled[entry.decoded.PromiseKey]; ok {
				continue
			}
		}

		// The system blob comes from the decoded view, or from parsing when there is none. Only txs
		// with a MsgPayForFibre are routed here and the shape check allows no other message,
		// so isFibreTx is always true.
		var (
			fibreTx   *tx.FibreTx
			isFibreTx = true
			err       error
		)
		if entry.decoded != nil {
			fibreTx, err = entry.decoded.FibreTx()
		} else {
			fibreTx, isFibreTx, err = fibretypes.TryParseFibreTx(rawTx)
		}
		if err != nil {
			logger.Error("synthesizing fibre tx", "tx", lazyTxHash(rawTx), "error", err)
			continue
		}
		if !isFibreTx {
			logger.Error("expected pay-for-fibre tx", "tx", lazyTxHash(rawTx))
			continue
		}

		// Branch the state per tx so a dropped tx leaves no partial writes
		// behind for the txs that follow it.
		txCtx, commit := ctx.CacheContext()
		txCtx = txCtx.WithTxBytes(rawTx)
		certKeys.Set(entry.decoded)

		ok, err := fsb.builder.AppendFibreTx(fibreTx)
		if err != nil {
			logger.Error("appending pay-for-fibre transaction to builder", "tx", lazyTxHash(rawTx), "error", err)
			continue
		}
		if !ok {
			logger.Debug("skipping pay-for-fibre tx because it was too large to fit in the square", "tx", lazyTxHash(rawTx))
			continue
		}

		txCtx, err = fsb.handler(txCtx, sdkTx, false)
		if err != nil {
			logger.Error(
				"filtering already checked pay-for-fibre transaction",
				"tx", lazyTxHash(rawTx),
				"error", err,
				"msgs", msgTypes(sdkTx),
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_pay_for_fibre_txs")
			if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
				logger.Error("reverting last pay-for-fibre transaction", "error", revertErr)
			}
			continue
		}

		// Settle the promise on the branch so later fibre txs are validated
		// against the escrow debit and processed-payment record it leaves
		// behind. A promise that cannot settle (overdraw, duplicate, out of
		// gas) would commit its blob to the square without payment in
		// FinalizeBlock, so drop the tx.
		if err := runMsgs(txCtx, sdkTx.GetMsgs(), fsb.msgRouter); err != nil {
			logger.Error(
				"dropping pay-for-fibre tx: promise cannot settle on the proposal state (e.g. escrow overdrawn or promise already processed)",
				"tx", lazyTxHash(rawTx),
				"error", err,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "unsettleable_pay_for_fibre_txs")
			if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
				logger.Error("reverting last pay-for-fibre transaction", "error", revertErr)
			}
			continue
		}
		commit()
		if entry.decoded != nil && entry.decoded.PromiseKeyed() {
			settled[entry.decoded.PromiseKey] = struct{}{}
		}

		pffMessageCount += len(sdkTx.GetMsgs())
		fibreTxs = append(fibreTxs, rawTx)
	}

	return fibreTxs
}

// lazyTxHash defers hashing a transaction until a log line is written, so
// lines below the logger's level cost nothing. It renders like the hash's
// tmbytes.HexBytes.
type lazyTxHash []byte

func (h lazyTxHash) String() string {
	return tmbytes.HexBytes(coretypes.Tx(h).Hash()).String()
}

func (h lazyTxHash) MarshalJSON() ([]byte, error) {
	return tmbytes.HexBytes(coretypes.Tx(h).Hash()).MarshalJSON()
}
