package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	abci "github.com/cometbft/cometbft/abci/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

var (
	finalizeFixtureMu sync.Mutex
	finalizeFixture   *proposalFixture
)

// finalize runs FinalizeBlock and Commit on a block that ProcessProposal
// accepted. FinalizeBlock writes into the app's root store, so the fixture
// cannot be reused afterwards.
func (fx *proposalFixture) finalize(t *testing.T, txs [][]byte) {
	t.Helper()
	fx.logs.Reset()
	resp, err := fx.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Txs:    txs,
		Height: fx.height,
		Time:   fx.blockTime,
		Hash:   fx.app.LastCommitID().Hash,
	})
	// An error here halts the chain: every validator voted for this block.
	require.NoError(t, err, "FinalizeBlock failed on a block ProcessProposal accepted")
	require.Len(t, resp.TxResults, len(txs))
	require.NotEmpty(t, resp.AppHash)
	for i, res := range resp.TxResults {
		if res.Codespace == sdkerrors.RootCodespace && res.Code == sdkerrors.ErrPanic.ABCICode() {
			t.Fatalf("FinalizeBlock recovered a panic in tx %d:\n%s", i, res.Log)
		}
	}
	if logged := fx.logs.String(); strings.Contains(logged, "caught panic") || strings.Contains(logged, "panic recovered") {
		t.Fatalf("FinalizeBlock recovered a panic:\n%s", logged)
	}
	_, err = fx.app.Commit()
	require.NoError(t, err, "Commit failed after FinalizeBlock")
}

// fuzzFinalizeBlock mutates the fixture proposal and, when ProcessProposal
// accepts a changed proposal with the data root a proposer would claim for it,
// finalizes and commits it.
func fuzzFinalizeBlock(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
	t.Helper()
	finalizeFixtureMu.Lock()
	defer finalizeFixtureMu.Unlock()
	if finalizeFixture == nil {
		finalizeFixture = newProposalFixture(t)
	}
	fx := finalizeFixture

	txs := mutateProposal(fx.txs, txIndex, op, offset, value, extra)
	unchanged := len(txs) == len(fx.txs)
	for i := range txs {
		unchanged = unchanged && bytes.Equal(txs[i], fx.txs[i])
	}
	// Proposals made only of txs PrepareProposal built are covered by the
	// ordinary tests; finalize only blocks the fuzzer changed.
	if unchanged {
		return
	}
	// A byzantine proposer controls both the txs and the data root it claims,
	// so claim the root of the mutated txs rather than the original one.
	dataRoot, squareSize := fx.dataRoot, fx.square
	if eds, err := da.ConstructEDS(txs, appconsts.Version, -1); err == nil {
		dah, err := da.NewDataAvailabilityHeader(eds)
		require.NoError(t, err)
		dataRoot, squareSize = dah.Hash(), uint64(eds.Width()/2)
	}
	if fx.processProposal(t, txs, dataRoot, squareSize) != abci.ResponseProcessProposal_ACCEPT {
		return
	}
	// Rebuild on the next input whatever happens below.
	finalizeFixture = nil
	fx.finalize(t, txs)
}

// TestFinalizeBlockSeeds checks the harness end to end: the unmodified
// proposal finalizes with every tx succeeding, and each seed mutation runs.
func TestFinalizeBlockSeeds(t *testing.T) {
	fx := newProposalFixture(t)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, fx.processProposal(t, fx.txs, fx.dataRoot, fx.square))
	fx.logs.Reset()
	resp, err := fx.app.FinalizeBlock(&abci.RequestFinalizeBlock{Txs: fx.txs, Height: fx.height, Time: fx.blockTime})
	require.NoError(t, err)
	for i, res := range resp.TxResults {
		require.Equal(t, abci.CodeTypeOK, res.Code, "tx %d: %s", i, res.Log)
	}

	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				fuzzFinalizeBlock(t, txIndex, op, 7, 0xff, []byte{0x0a, 0x00})
			})
		}
	}
}

// FuzzFinalizeBlockMutatedProposal mutates the txs of a valid proposal and,
// whenever ProcessProposal accepts the changed proposal, runs FinalizeBlock and
// Commit on it. Every validator executes a block that ProcessProposal
// accepted, so FinalizeBlock must not return an error, recover a panic, or
// return a result per tx that does not match the block.
func FuzzFinalizeBlockMutatedProposal(f *testing.F) {
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			f.Add(txIndex, op, uint32(3), byte(0x80), []byte{0x0a, 0x00})
			f.Add(txIndex, op, uint32(1<<16), byte(0x01), []byte("\x12\x04\x08\x01\x10\x02"))
		}
	}
	f.Fuzz(func(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
		if len(extra) > 1<<20 {
			return
		}
		fuzzFinalizeBlock(t, txIndex, op, offset, value, extra)
	})
}
