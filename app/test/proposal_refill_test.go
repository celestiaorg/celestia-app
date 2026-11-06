package app_test

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// Transactions that fail the ante handler in PrepareProposal must not keep the
// byte budget they were admitted under. Lower-priority candidates of the same
// class must be able to use the bytes they release.
func TestPrepareProposalRefillsBudgetReleasedByDroppedTxs(t *testing.T) {
	sendFillers := []string{"send-0", "send-1"}
	pfbFillers := []string{"pfb-0", "pfb-1", "pfb-2", "pfb-3"}
	accounts := append(append([]string{"chain"}, sendFillers...), pfbFillers...)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	signerAccounts := make([]*user.Account, len(accounts))
	for i, info := range queryAccountInfo(testApp, accounts, kr) {
		signerAccounts[i] = user.NewAccount(accounts[i], info.AccountNum, info.Sequence)
	}
	signer, err := user.NewSigner(kr, testApp.GetTxConfig(), testutil.ChainID, signerAccounts...)
	require.NoError(t, err)

	send := func(from string, seq uint64, opts ...user.TxOption) []byte {
		require.NoError(t, signer.SetSequence(from, seq))
		addr := signer.Account(from).Address()
		msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		opts = append([]user.TxOption{user.SetGasLimitAndGasPrice(1_000_000, 0.1)}, opts...)
		tx, _, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
		require.NoError(t, err)
		return tx
	}
	pfb := func(from string, seq uint64, size int, opts ...user.TxOption) []byte {
		require.NoError(t, signer.SetSequence(from, seq))
		blob, err := share.NewBlob(share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize)),
			bytes.Repeat([]byte{1}, size), appconsts.DefaultShareVersion, nil)
		require.NoError(t, err)
		opts = append([]user.TxOption{user.SetGasLimitAndGasPrice(10_000_000, 0.1)}, opts...)
		tx, _, err := signer.CreatePayForBlobs(from, []*share.Blob{blob}, opts...)
		require.NoError(t, err)
		return tx
	}

	height := testApp.LastBlockHeight() + 1
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
				pfb("chain", 0, 100),
				send("chain", 1),
				pfb("chain", 2, bigBlob),
				send("chain", 3),
				pfb("chain", 4, bigBlob),
			},
			sendFillers: sendFillers,
		},
		{
			name: "expired blob txs",
			chain: [][]byte{
				pfb("chain", 0, 100),
				pfb("chain", 1, bigBlob, expired),
				pfb("chain", 2, bigBlob, expired),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Lower-priority candidates from other signers, after the chain in
			// mempool order.
			sends := make([][]byte, 0, len(tc.sendFillers))
			for _, f := range tc.sendFillers {
				sends = append(sends, send(f, 0, lowFee))
			}
			pfbs := make([][]byte, 0, len(pfbFillers))
			for _, f := range pfbFillers {
				pfbs = append(pfbs, pfb(f, 0, 1000, lowFee))
			}
			lower := append(append([][]byte{}, sends...), pfbs...)

			// The budget admits exactly the chain in the byte prefilter, and
			// the fillers of each class fit in the bytes released by the
			// dropped chain txs of that class.
			size := func(txs ...[]byte) int64 { return coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(txs)) }
			budget := size(tc.chain...)
			var droppedSends, droppedPFBs int64
			for _, tx := range tc.chain[1:] {
				if _, isBlob, _ := blobtx.UnmarshalBlobTx(tx); isBlob {
					droppedPFBs += size(tx)
				} else {
					droppedSends += size(tx)
				}
			}
			require.LessOrEqual(t, size(sends...), droppedSends)
			require.LessOrEqual(t, size(pfbs...), droppedPFBs)

			blockTime := time.Now()
			prepared, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
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

			processed, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
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
