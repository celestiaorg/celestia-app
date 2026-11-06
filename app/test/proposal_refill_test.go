package app_test

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// Transactions that fail the ante handler in PrepareProposal must not keep the
// byte budget they were admitted under. A signer's chain that alternates blob
// and normal transactions cannot be included in one block past its first
// transaction, because the square orders normal transactions first. The
// proposer must refill the budget that chain released with lower-priority
// candidates.
func TestPrepareProposalRefillsBudgetReleasedByDroppedTxs(t *testing.T) {
	fillers := []string{"filler-0", "filler-1", "filler-2", "filler-3"}
	proposer, validator, txs := newProposalAppPair(t, 16, append([]string{"chain"}, fillers...))

	height := proposer.LastBlockHeight() + 1
	const bigBlob = 20_000
	for _, tc := range []struct {
		name  string
		chain [][]byte
	}{
		{
			name: "mixed-class nonce chain",
			chain: [][]byte{
				txs.pfb("chain", 0, 100),
				txs.send("chain", 1),
				txs.pfb("chain", 2, bigBlob),
				txs.send("chain", 3),
				txs.pfb("chain", 4, bigBlob),
			},
		},
		{
			name: "expired suffix",
			chain: [][]byte{
				txs.pfb("chain", 0, 100),
				pfbWithOpts(t, txs, "chain", 1, bigBlob, user.SetTimeoutHeight(uint64(height-1))),
				pfbWithOpts(t, txs, "chain", 2, bigBlob, user.SetTimeoutHeight(uint64(height-1))),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Lower-priority candidates from other signers, after the chain in
			// mempool order.
			lower := make([][]byte, 0, 2*len(fillers))
			for _, f := range fillers {
				lower = append(lower, txs.send(f, 0, user.SetGasLimitAndGasPrice(1_000_000, 0.01)))
			}
			for _, f := range fillers {
				lower = append(lower, pfbWithOpts(t, txs, f, 1, 1000, user.SetGasLimitAndGasPrice(10_000_000, 0.01)))
			}

			// The budget admits exactly the chain in the byte prefilter.
			budget := coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(tc.chain))
			require.Less(t, coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(lower)), budget-coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(tc.chain[:1])))

			blockTime := testutil.GenesisTime.Add(time.Minute)
			prepared, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{
				Height: height, Time: blockTime, Txs: append(append([][]byte{}, tc.chain...), lower...), MaxTxBytes: budget,
			})
			require.NoError(t, err)

			require.True(t, containsTx(prepared.Txs, tc.chain[0]), "first chain tx was dropped")
			for i, tx := range tc.chain[1:] {
				require.False(t, containsTx(prepared.Txs, tx), "chain tx %d cannot be valid in this block", i+1)
			}
			for i, tx := range lower {
				require.True(t, containsTx(prepared.Txs, tx), "lower-priority candidate %d was not refilled (kept %d txs)", i, len(prepared.Txs))
			}
			require.Len(t, prepared.Txs, 1+len(lower))
			require.LessOrEqual(t, coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(prepared.Txs)), budget)

			processed, err := validator.ProcessProposal(&abci.RequestProcessProposal{
				Height: height, Time: blockTime, Txs: prepared.Txs,
				SquareSize: prepared.SquareSize, DataRootHash: prepared.DataRootHash,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
		})
	}
}

func containsTx(txs [][]byte, tx []byte) bool {
	return slices.ContainsFunc(txs, func(candidate []byte) bool { return bytes.Equal(candidate, tx) })
}

func pfbWithOpts(t *testing.T, txs corpusTxs, from string, seq uint64, size int, opts ...user.TxOption) []byte {
	t.Helper()
	require.NoError(t, txs.signer.SetSequence(from, seq))
	opts = append([]user.TxOption{user.SetGasLimitAndGasPrice(10_000_000, 0.1)}, opts...)
	tx, _, err := txs.signer.CreatePayForBlobs(from, txs.blobs(size), opts...)
	require.NoError(t, err)
	return tx
}
