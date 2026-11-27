package app_test

import (
	"math/rand"
	"testing"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/encoding"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v12/test/util"
	"github.com/celestiaorg/celestia-app/v12/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v12/test/util/testfactory"
	zkismtypes "github.com/celestiaorg/celestia-app/v12/x/zkism/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// wantBlockLimits are the per-block limits PrepareProposal applies to SDK txs.
var wantBlockLimits = abci.BlockLimits{
	MaxSdkGas:          appconsts.MaxSDKGas,
	MaxSdkMessages:     appconsts.MaxSDKMessages,
	MaxZkProofMessages: appconsts.MaxZKProofMessages,
	MaxIsmSignatures:   appconsts.MaxISMSignatures,
}

// TestBlockLimits checks that Info and FinalizeBlock report the per-block
// limits, so the mempool has them after a restart and after every block.
func TestBlockLimits(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "a")

	infoRes, err := testApp.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, wantBlockLimits, infoRes.BlockLimits)

	finalizeRes := commitBlock(t, testApp)
	require.Equal(t, wantBlockLimits, finalizeRes.BlockLimits)
}

// TestCheckTxUsage checks that CheckTx reports what a tx uses of the per-block
// limits: an SDK tx counts towards them, a blob tx and a PayForFibre tx do not.
func TestCheckTxUsage(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(6)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

	t.Run("an SDK tx reports its gas limit and its message count", func(t *testing.T) {
		const gasLimit = 300_000
		const msgCount = 3
		addr := testfactory.GetAddress(kr, accounts[0])
		amount := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))
		msgs := make([]sdk.Msg, 0, msgCount)
		for range msgCount {
			msgs = append(msgs, banktypes.NewMsgSend(addr, addr, amount))
		}
		rawTx, _, err := newSigner(0).CreateTx(msgs, user.SetGasLimit(gasLimit), user.SetFee(gasLimit))
		require.NoError(t, err)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		require.Equal(t, abci.TxUsage{SdkGas: gasLimit, SdkMessages: msgCount}, res.Usage)

		// The mempool ignores recheck responses, so a recheck reports no usage.
		// Rechecks run against the state of a new block.
		commitBlock(t, testApp)
		recheckRes, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_Recheck})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, recheckRes.Code, recheckRes.Log)
		require.Equal(t, abci.TxUsage{}, recheckRes.Usage)
	})

	t.Run("an SDK tx reports its ZK proof messages", func(t *testing.T) {
		const gasLimit = 1_000_000
		const proofCount = 2
		addr := testfactory.GetAddress(kr, accounts[3])
		msgs := make([]sdk.Msg, 0, proofCount)
		for i := range proofCount {
			msgs = append(msgs, &zkismtypes.MsgUpdateInterchainSecurityModule{
				Id:     util.CreateMockHexAddress("ism", int64(i+1)),
				Proof:  make([]byte, zkismtypes.ProofBytesLen),
				Signer: addr.String(),
			})
		}
		rawTx, _, err := newSigner(3).CreateTx(msgs, user.SetGasLimit(gasLimit), user.SetFee(gasLimit))
		require.NoError(t, err)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		wantUsage := abci.TxUsage{SdkGas: gasLimit, SdkMessages: proofCount, ZkProofMessages: proofCount}
		require.Equal(t, wantUsage, res.Usage)
	})

	t.Run("an SDK tx reports its ISM signatures", func(t *testing.T) {
		const sigCount = 3
		gasLimit := ismGasLimit(sigCount)
		addr := testfactory.GetAddress(kr, accounts[4])
		msg := &hyperlanetypes.MsgProcessMessage{
			Relayer:  addr.String(),
			Metadata: ismMetadataHex(sigCount),
			Message:  "0x",
		}
		rawTx, _, err := newSigner(4).CreateTx([]sdk.Msg{msg}, user.SetGasLimit(gasLimit), user.SetFee(gasLimit))
		require.NoError(t, err)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		wantUsage := abci.TxUsage{SdkGas: gasLimit, SdkMessages: 1, IsmSignatures: sigCount}
		require.Equal(t, wantUsage, res.Usage)
	})

	t.Run("an SDK tx reports the messages a MsgExec dispatches", func(t *testing.T) {
		const gasLimit = 300_000
		const nestedCount = 3
		addr := testfactory.GetAddress(kr, accounts[5])
		amount := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))
		nested := make([]sdk.Msg, 0, nestedCount)
		for range nestedCount {
			nested = append(nested, banktypes.NewMsgSend(addr, addr, amount))
		}
		exec := authz.NewMsgExec(addr, nested)
		rawTx, _, err := newSigner(5).CreateTx([]sdk.Msg{&exec}, user.SetGasLimit(gasLimit), user.SetFee(gasLimit))
		require.NoError(t, err)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		// One top-level message, but the block executes the MsgExec and the
		// messages it dispatches.
		wantUsage := abci.TxUsage{SdkGas: gasLimit, SdkMessages: 1 + nestedCount}
		require.Equal(t, wantUsage, res.Usage)
	})

	t.Run("a blob tx reports no usage", func(t *testing.T) {
		const gasLimit = 200_000
		blobs := blobfactory.ManyRandBlobs(rand.New(rand.NewSource(1)), 1000)
		rawTx, _, err := newSigner(1).CreatePayForBlobs(accounts[1], blobs, user.SetGasLimit(gasLimit), user.SetFee(gasLimit))
		require.NoError(t, err)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		require.Equal(t, abci.TxUsage{}, res.Usage)
	})

	t.Run("a PayForFibre tx reports no usage", func(t *testing.T) {
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[2]), 1_000_000)
		rawTx := newSignedPayForFibreTx(t, newSigner(2), accounts[2], true)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
		require.Equal(t, abci.TxUsage{}, res.Usage)
	})

	t.Run("a rejected SDK tx reports no usage", func(t *testing.T) {
		// The fee is below the minimum, so the tx decodes and passes the
		// limit checks but the ante handler rejects it.
		const gasLimit = 300_000
		addr := testfactory.GetAddress(kr, accounts[0])
		amount := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))
		msgs := []sdk.Msg{banktypes.NewMsgSend(addr, addr, amount)}
		rawTx, _, err := newSigner(0).CreateTx(msgs, user.SetGasLimit(gasLimit), user.SetFee(1))
		require.NoError(t, err)

		res, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.NotEqual(t, abci.CodeTypeOK, res.Code)
		require.Equal(t, abci.TxUsage{}, res.Usage)
	})
}
