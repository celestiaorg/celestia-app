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
	// maxSquareSize and subtreeRootThreshold build a fresh square for each
	// round of Fill.
	maxSquareSize        int
	subtreeRootThreshold int
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

		maxSquareSize:        maxSquareSize,
		subtreeRootThreshold: subtreeRootThreshold,
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

// maxFillRounds bounds how many times Fill rebuilds the square to refill byte
// budget released by dropped txs. Each round runs the ante handler at most
// once per candidate, so Fill does at most maxFillRounds times the work of a
// single pass.
const maxFillRounds = 3

// Fill selects the txs of the proposal from txs, which is in mempool priority
// order, and appends them to the square.
//
// The byte budget is assigned in priority order before the txs are validated.
// A tx that is then dropped (it fails the ante handler, for example because a
// signer's nonce chain alternates normal and blob txs, which the square
// reorders normal-first, or because it expired) would otherwise keep its share
// of the budget and leave the proposal underfilled. So after a round that
// dropped a tx while other candidates were left out of the budget, Fill starts
// over without the dropped txs, letting the next candidates in priority order
// take their bytes. Each round starts from the same state and builds a fresh
// square, so every round on its own yields a valid proposal; Fill keeps the one
// with the most bytes.
func (fsb *FilteredSquareBuilder) Fill(ctx sdk.Context, txs [][]byte, maxTxBytes int64) [][]byte {
	logger := ctx.Logger().With("app/filtered-square-builder")

	// Size each tx by its protobuf-framed length (tag + length prefix + bytes),
	// matching how CometBFT validates the returned list. Raw len(tx) under-counts
	// the framing, so a list that fits here could still be rejected by Core.
	sizes := make([]int64, len(txs))
	for i, tx := range txs {
		sizes[i] = coretypes.ComputeProtoSizeForTxs([]coretypes.Tx{tx})
	}

	var (
		dropped = make([]bool, len(txs))
		best    fillResult
	)
	for round := range maxFillRounds {
		selected, excluded := selectWithinBytes(logger, txs, sizes, dropped, maxTxBytes)

		builder := fsb.builder
		if round > 0 {
			var err error
			builder, err = square.NewBuilder(fsb.maxSquareSize, fsb.subtreeRootThreshold)
			if err != nil {
				// Unreachable: the constructor built a square with the same parameters.
				logger.Error("creating square builder for refill round", "err", err)
				break
			}
		}
		fsb.builder = builder

		roundCtx, commit := ctx.CacheContext()
		result := fsb.fillRound(roundCtx, txs, selected, round == 0)
		result.builder, result.commit = builder, commit
		for _, i := range result.keptIdx {
			result.bytes += sizes[i]
		}
		if round == 0 || result.bytes > best.bytes {
			best = result
		}

		kept := make([]bool, len(txs))
		for _, i := range result.keptIdx {
			kept[i] = true
		}
		anyDropped := false
		for _, i := range selected {
			if !kept[i] {
				dropped[i] = true
				anyDropped = true
			}
		}
		// Another round helps only if it can admit a candidate this one left out.
		if !anyDropped || !excluded {
			break
		}
	}

	best.commit()
	fsb.builder = best.builder
	fsb.keptBlobTxs = best.keptBlobTxs
	return best.txs
}

// fillResult is the outcome of one round of Fill.
type fillResult struct {
	txs [][]byte
	// keptIdx holds the index into Fill's input of each tx in txs.
	keptIdx     []int
	keptBlobTxs []*tx.BlobTx
	bytes       int64
	builder     *square.Builder
	commit      func()
}

// selectWithinBytes returns, in input order, the indices of the txs not yet
// dropped whose cumulative size fits maxTxBytes, and whether a tx not yet
// dropped was left out. Required because the data square can be configured
// larger than block.MaxBytes. The kept list of a round is a subset of the
// selection, so the bound carries over.
//
// A non-positive maxTxBytes means "no limit" — CometBFT always sets a
// positive value in production, but some tests construct requests directly
// and leave it at zero.
func selectWithinBytes(logger log.Logger, txs [][]byte, sizes []int64, dropped []bool, maxTxBytes int64) (selected []int, excluded bool) {
	selected = make([]int, 0, len(txs))
	var currentTxBytes int64
	for i := range txs {
		if dropped[i] {
			continue
		}
		if maxTxBytes > 0 && currentTxBytes+sizes[i] > maxTxBytes {
			logger.Debug("skipping tx because it was too large to fit in the block", "tx", lazyTxHash(txs[i]))
			excluded = true
			continue
		}
		currentTxBytes += sizes[i]
		selected = append(selected, i)
	}
	return selected, excluded
}

// fillRound appends the selected txs that pass validation to fsb.builder,
// normal txs first, then blob txs, then pay-for-fibre txs, executing their
// ante handlers on ctx in that order.
func (fsb *FilteredSquareBuilder) fillRound(ctx sdk.Context, txs [][]byte, selected []int, preverify bool) fillResult {
	logger := ctx.Logger().With("app/filtered-square-builder")

	// note that there is an additional filter step for tx size of raw txs here
	normalIdx, blobIdx, payForFibreTxs, pffIdx := separateTxIndices(fsb.txConfig, txs, selected)
	dec := fsb.txConfig.TxDecoder()

	// Warm the signature cache across every CPU before filling. Failures are
	// not recorded, so the fill still performs and decides each check itself.
	if preverify && fsb.preverifyPFF != nil {
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
		sdkGas          uint64
		pfbMessageCount = 0
		keptNormal      = make([]int, 0, len(normalIdx))
		keptBlob        = make([]int, 0, len(blobIdx))
	)

	for _, idx := range normalIdx {
		tx := txs[idx]
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
		txGas := txGasLimit(sdkTx)
		if !fitsSDKGas(sdkGas, txGas) {
			logger.Debug("skipping tx because the max SDK gas was reached", "tx", lazyTxHash(tx))
			continue
		}

		fits, err := fsb.builder.Fits(square.NewClassifiedTx(tx))
		if err != nil {
			logger.Debug("skipping tx due to error", "tx", lazyTxHash(tx), "err", err)
			continue
		}
		if !fits {
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
			continue
		}
		// Fits ran on the same builder state, so this append succeeds.
		if !fsb.builder.AppendTx(tx) {
			logger.Error("appending tx that fit the square", "tx", lazyTxHash(tx))
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
		sdkGas += txGas
		keptNormal = append(keptNormal, idx)
	}

	var keptBlobTxs []*tx.BlobTx
	for _, idx := range blobIdx {
		rawTx := txs[idx]
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

		fits, err := fsb.builder.Fits(square.ClassifiedTx{Bytes: rawTx, BlobTx: bTx})
		if err != nil {
			logger.Debug("skipping blob tx due to error", "tx", lazyTxHash(bTx.Tx), "err", err)
			continue
		}
		if !fits {
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
			continue
		}
		// Fits ran on the same builder state, so this append succeeds.
		if ok, err := fsb.builder.AppendBlobTx(bTx); err != nil || !ok {
			logger.Error("appending blob tx that fit the square", "tx", lazyTxHash(bTx.Tx), "err", err)
			continue
		}
		commit()
		ctx = txCtx.WithMultiStore(ctx.MultiStore()).WithEventManager(ctx.EventManager())

		pfbMessageCount += len(sdkTx.GetMsgs())
		keptBlob = append(keptBlob, idx)
		keptBlobTxs = append(keptBlobTxs, bTx)
	}

	keptFibre := processFibreTxsForSquare(fsb, ctx, dec, payForFibreTxs)

	n, m := len(keptNormal), len(keptBlob)
	result := fillResult{keptIdx: make([]int, 0, n+m+len(keptFibre))}
	result.keptIdx = append(result.keptIdx, keptNormal...)
	result.keptIdx = append(result.keptIdx, keptBlob...)
	for _, i := range keptFibre {
		result.keptIdx = append(result.keptIdx, pffIdx[i])
	}
	// UnmarshalBlobTx only accepts canonically encoded blob txs, so the raw bytes
	// are identical to re-marshaling the decoded blob txs and can be reused.
	result.txs = make([][]byte, len(result.keptIdx))
	for i, idx := range result.keptIdx {
		result.txs[i] = txs[idx]
	}

	// Record where the decoded blob txs ended up so ProcessProposal reuses
	// them. Only blob txs occupy this range; normal and fibre txs stay nil.
	result.keptBlobTxs = make([]*tx.BlobTx, len(result.txs))
	copy(result.keptBlobTxs[n:n+m], keptBlobTxs)
	return result
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
	all := make([]int, len(rawTxs))
	for i := range all {
		all[i] = i
	}
	normalIdx, blobIdx, payForFibreTxs, _ := separateTxIndices(txConfig, rawTxs, all)
	normalTxs = make([][]byte, len(normalIdx))
	for i, idx := range normalIdx {
		normalTxs[i] = rawTxs[idx]
	}
	rawBlobTxs = make([][]byte, len(blobIdx))
	for i, idx := range blobIdx {
		rawBlobTxs[i] = rawTxs[idx]
	}
	return normalTxs, rawBlobTxs, payForFibreTxs
}

// separateTxIndices is separateTxsDecoded over the txs at the given indices of
// rawTxs. It returns indices into rawTxs; pffIdx is index aligned with
// payForFibreTxs.
func separateTxIndices(txConfig client.TxConfig, rawTxs [][]byte, indices []int) (normalIdx, blobIdx []int, payForFibreTxs []decodedFibreTx, pffIdx []int) {
	normalIdx = make([]int, 0, len(indices))
	blobIdx = make([]int, 0, len(indices))
	payForFibreTxs = make([]decodedFibreTx, 0, len(indices))
	pffIdx = make([]int, 0, len(indices))
	dec := txConfig.TxDecoder()

	for _, idx := range indices {
		rawTx := rawTxs[idx]
		// this check in theory shouldn't get hit, as txs should be filtered
		// in CheckTx. However in tests we're inserting too large of txs
		// therefore also filter here.
		if len(rawTx) > appconsts.MaxTxSize {
			continue
		}

		// The same predicate as UnmarshalBlobTx's isBlob, without decoding the blobs.
		if tx.HasBlobTxTypeID(rawTx) {
			blobIdx = append(blobIdx, idx)
			continue
		}

		// Fill decodes these lazily and stops at each cap; a failed peek takes the full decode.
		if isPFF, ok := firstMsgIsPayForFibre(rawTx); ok {
			if isPFF {
				payForFibreTxs = append(payForFibreTxs, decodedFibreTx{raw: rawTx})
				pffIdx = append(pffIdx, idx)
			} else {
				normalIdx = append(normalIdx, idx)
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
			pffIdx = append(pffIdx, idx)
			continue
		}

		normalIdx = append(normalIdx, idx)
	}
	return normalIdx, blobIdx, payForFibreTxs, pffIdx
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
// system blob, validate, append to builder. Returns the positions in
// payForFibreTxs of the accepted fibre txs.
func processFibreTxsForSquare(fsb *FilteredSquareBuilder, ctx sdk.Context, dec sdk.TxDecoder, payForFibreTxs []decodedFibreTx) []int {
	logger := ctx.Logger().With("app/filtered-square-builder")
	var pffMessageCount int
	fibreTxs := make([]int, 0, min(len(payForFibreTxs), appconsts.MaxPayForFibreMessages))
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

		fits, err := fsb.builder.Fits(square.ClassifiedTx{Bytes: rawTx, FibreTx: fibreTx})
		if err != nil {
			logger.Error("sizing pay-for-fibre transaction", "tx", lazyTxHash(rawTx), "error", err)
			continue
		}
		if !fits {
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
			continue
		}
		// Fits ran on the same builder state, so this append succeeds.
		if ok, err := fsb.builder.AppendFibreTx(fibreTx); err != nil || !ok {
			logger.Error("appending pay-for-fibre transaction that fit the square", "tx", lazyTxHash(rawTx), "error", err)
			continue
		}
		commit()
		if entry.decoded != nil && entry.decoded.PromiseKeyed() {
			settled[entry.decoded.PromiseKey] = struct{}{}
		}

		pffMessageCount += len(sdkTx.GetMsgs())
		fibreTxs = append(fibreTxs, i)
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
