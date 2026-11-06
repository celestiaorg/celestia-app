package app_test

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// Transactions that fail the ante handler in PrepareProposal must not keep the
// byte budget they were admitted under. Lower-priority candidates of the same
// class must be able to use the bytes they release.
func TestPrepareProposalRefillsBudgetReleasedByDroppedTxs(t *testing.T) {
	sendFillers := []string{"send-0", "send-1"}
	pfbFillers := []string{"pfb-0", "pfb-1", "pfb-2", "pfb-3"}
	accounts := append(append([]string{"chain"}, sendFillers...), pfbFillers...)
	proposer, validator, txs := newProposalAppPair(t, 16, accounts)

	height := proposer.LastBlockHeight() + 1
	const bigBlob = 20_000
	expired := user.SetTimeoutHeight(uint64(height - 1))
	lowFee := user.SetGasLimitAndGasPrice(1_000_000, 0.01)
	for _, tc := range []struct {
		name        string
		chain       [][]byte
		sendFillers []string
	}{
		{
			// The square orders normal txs before blob txs, so only the first
			// tx of a chain that alternates blob and normal txs is valid.
			name: "mixed-class nonce chain",
			chain: [][]byte{
				txs.pfb("chain", 0, 100),
				txs.send("chain", 1),
				txs.pfb("chain", 2, bigBlob),
				txs.send("chain", 3),
				txs.pfb("chain", 4, bigBlob),
			},
			sendFillers: sendFillers,
		},
		{
			name: "expired blob txs",
			chain: [][]byte{
				txs.pfb("chain", 0, 100),
				txs.pfbWithOpts("chain", 1, []int{bigBlob}, expired),
				txs.pfbWithOpts("chain", 2, []int{bigBlob}, expired),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Lower-priority candidates from other signers, after the chain in
			// mempool order.
			sends := make([][]byte, 0, len(tc.sendFillers))
			pfbs := make([][]byte, 0, len(pfbFillers))
			for _, f := range tc.sendFillers {
				sends = append(sends, txs.send(f, 0, lowFee))
			}
			for _, f := range pfbFillers {
				pfbs = append(pfbs, txs.pfbWithOpts(f, 0, []int{1000}, lowFee))
			}
			lower := append(append([][]byte{}, sends...), pfbs...)

			// The budget admits exactly the chain in the byte prefilter, and
			// the fillers of each class fit in the bytes released by the
			// dropped chain txs of that class.
			size := func(txs ...[]byte) int64 { return coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(txs)) }
			budget := size(tc.chain...)
			var droppedSends, droppedPFBs int64
			for _, tx := range tc.chain[1:] {
				if blobtx.HasBlobTxTypeID(tx) {
					droppedPFBs += size(tx)
				} else {
					droppedSends += size(tx)
				}
			}
			require.LessOrEqual(t, size(sends...), droppedSends)
			require.LessOrEqual(t, size(pfbs...), droppedPFBs)

			blockTime := testutil.GenesisTime.Add(time.Minute)
			prepared, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{
				Height: height, Time: blockTime, Txs: append(append([][]byte{}, tc.chain...), lower...), MaxTxBytes: budget,
			})
			require.NoError(t, err)

			require.True(t, containsTx(prepared.Txs, tc.chain[0]), "first chain tx was dropped")
			for i, tx := range tc.chain[1:] {
				require.False(t, containsTx(prepared.Txs, tx), "chain tx %d cannot be valid in this block", i+1)
			}
			for i, tx := range sends {
				require.True(t, containsTx(prepared.Txs, tx), "lower-priority send %d was not refilled", i)
			}
			for i, tx := range pfbs {
				require.True(t, containsTx(prepared.Txs, tx), "lower-priority pfb %d was not refilled", i)
			}
			require.Len(t, prepared.Txs, 1+len(lower))
			require.LessOrEqual(t, size(prepared.Txs...), budget)

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
