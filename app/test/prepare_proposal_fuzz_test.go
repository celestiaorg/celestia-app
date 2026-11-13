package app_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// maxBatchMutations bounds the edits applied to one mempool batch so each
// input does a bounded amount of work.
const maxBatchMutations = 8

// mutateBatch applies up to maxBatchMutations edits to txs. Each edit consumes
// four bytes of ops: tx index, operation, offset and value; see mutateProposal.
func mutateBatch(txs [][]byte, ops, extra []byte) [][]byte {
	for i := 0; i+4 <= len(ops) && i/4 < maxBatchMutations; i += 4 {
		txs = mutateProposal(txs, ops[i], ops[i+1], uint32(ops[i+2])<<8|uint32(ops[i+3]), ops[i+3], extra)
	}
	return txs
}

// fuzzPrepareProposalBatch runs PrepareProposal on a mempool batch built from
// valid txs and attacker-chosen bytes, then runs ProcessProposal on the result.
// Mempool txs are untrusted, so PrepareProposal must drop whatever it cannot
// include, and the block an honest proposer builds must always be accepted.
// Otherwise one crafted tx in the mempool stops honest proposers from making
// progress.
func fuzzPrepareProposalBatch(t *testing.T, ops, extra []byte) {
	t.Helper()
	if len(extra) > appconsts.MaxTxSize {
		return
	}
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	fixtureOnce.Do(func() { fixture = newProposalFixture(t) })
	fx := fixture

	txs := mutateBatch(fx.mempool, ops, extra)
	if knownAntePanic(fx.app.GetTxConfig().TxDecoder(), txs) {
		return
	}
	fx.logs.Reset()
	resp, err := fx.app.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:        txs,
		Height:     fx.height,
		Time:       fx.blockTime,
		MaxTxBytes: int64(appconsts.BlockMaxBytes),
	})
	require.NoError(t, err)
	if logged := fx.logs.String(); strings.Contains(logged, "panic") {
		t.Fatalf("PrepareProposal recovered a panic:\n%s", logged)
	}
	status := fx.processProposal(t, resp.Txs, resp.DataRootHash, resp.SquareSize)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status,
		"ProcessProposal rejected the block PrepareProposal built:\n%s", fx.logs.String())
}

// TestPrepareProposalBatchSeeds keeps the fuzz seeds meaningful: the
// unmodified batch and each single mutation produce an accepted block.
func TestPrepareProposalBatchSeeds(t *testing.T) {
	fuzzPrepareProposalBatch(t, nil, nil)
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				fuzzPrepareProposalBatch(t, []byte{txIndex, op, 0, 7}, []byte{0x0a, 0x00})
			})
		}
	}
}

// FuzzPrepareProposalMalformedBatch feeds PrepareProposal mempool batches that
// mix valid send, pay-for-fibre and blob txs with mutated, duplicated,
// reordered and arbitrary txs. It fails if PrepareProposal recovers a panic or
// if ProcessProposal rejects the block PrepareProposal returns.
func FuzzPrepareProposalMalformedBatch(f *testing.F) {
	f.Add([]byte{}, []byte{})
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			f.Add([]byte{txIndex, op, 0, 3}, []byte{0x0a, 0x00})
			f.Add([]byte{txIndex, op, 1, 0, txIndex + 1, 6, 0, 0}, []byte("\x12\x04\x08\x01\x10\x02"))
		}
	}
	// Duplicate every tx, then corrupt the copies.
	f.Add([]byte{0, 6, 0, 0, 1, 6, 0, 0, 2, 6, 0, 0, 3, 6, 0, 0, 4, 1, 0, 9, 5, 1, 0, 9, 6, 1, 0, 9, 7, 1, 0, 9}, []byte{})
	f.Fuzz(func(t *testing.T, ops, extra []byte) {
		fuzzPrepareProposalBatch(t, ops, extra)
	})
}
