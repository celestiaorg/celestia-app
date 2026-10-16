package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// The mempool supplies priority order. PrepareProposal preserves that order
// within each class. The byte budget uses input order; the square builder
// considers normal transactions before blob transactions.
func TestPrepareProposalCapacityOrder(t *testing.T) {
	proposer, validator, txs := newProposalAppPair(t, 16, []string{"send-high", "send-low", "blob-high", "blob-low"})
	highSend := txs.send("send-high", 0, user.SetGasLimitAndGasPrice(1_000_000, 0.2))
	lowSend := txs.send("send-low", 0, user.SetGasLimitAndGasPrice(1_000_000, 0.1))
	highBlob, _, err := txs.signer.CreatePayForBlobs("blob-high", txs.blobs(1000), user.SetGasLimitAndGasPrice(1_000_000, 0.2))
	require.NoError(t, err)
	lowBlob, _, err := txs.signer.CreatePayForBlobs("blob-low", txs.blobs(1000), user.SetGasLimitAndGasPrice(1_000_000, 0.1))
	require.NoError(t, err)
	fullBlob, _, err := txs.signer.CreatePayForBlobs("blob-high", txs.blobs(share.AvailableBytesFromSparseShares(252)), user.SetGasLimitAndGasPrice(10_000_000, 0.2))
	require.NoError(t, err)

	framedSize := func(tx []byte) int64 {
		return coretypes.ComputeProtoSizeForTxs([]coretypes.Tx{tx})
	}
	for _, tc := range []struct {
		name   string
		input  [][]byte
		budget int64
		want   [][]byte
	}{
		{"normal priority order", [][]byte{highSend, lowSend}, framedSize(highSend), [][]byte{highSend}},
		{"blob priority order", [][]byte{highBlob, lowBlob}, framedSize(highBlob), [][]byte{highBlob}},
		{"byte budget follows input priority", [][]byte{highBlob, lowSend}, framedSize(highBlob), [][]byte{highBlob}},
		{"both classes fit", [][]byte{highBlob, lowSend}, framedSize(highBlob) + framedSize(lowSend), [][]byte{lowSend, highBlob}},
		{"large blob fits alone", [][]byte{fullBlob}, framedSize(fullBlob), [][]byte{fullBlob}},
		{"square preserves blob priority", [][]byte{fullBlob, lowBlob}, framedSize(fullBlob) + framedSize(lowBlob), [][]byte{fullBlob}},
		{"square gives normal tx precedence", [][]byte{fullBlob, lowSend}, framedSize(fullBlob) + framedSize(lowSend), [][]byte{lowSend}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			height := proposer.LastBlockHeight() + 1
			blockTime := testutil.GenesisTime.Add(time.Minute)
			prepared, err := proposer.PrepareProposal(&abci.RequestPrepareProposal{
				Height: height, Time: blockTime, Txs: tc.input, MaxTxBytes: tc.budget,
			})
			require.NoError(t, err)
			require.Len(t, prepared.Txs, len(tc.want))
			for i := range tc.want {
				require.True(t, bytes.Equal(tc.want[i], prepared.Txs[i]), "unexpected transaction at index %d", i)
			}

			processed, err := validator.ProcessProposal(&abci.RequestProcessProposal{
				Height: height, Time: blockTime, Txs: prepared.Txs,
				SquareSize: prepared.SquareSize, DataRootHash: prepared.DataRootHash,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
		})
	}
}
