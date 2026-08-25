package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

const pffTestEscrowAmount = 10_000_000

// TestPayForFibreReplayGasParity verifies cached and uncached txs use the same gas.
func TestPayForFibreReplayGasParity(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	sendTx, pffTxs := buildGasParityTxs(t, testApp, kr, newSigner, accounts)

	// Cache the first PFF tx through CheckTx.
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: pffTxs[0], Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)

	finalizeResp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   time.Now(),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    [][]byte{sendTx, pffTxs[0], pffTxs[1]},
	})
	require.NoError(t, err)
	require.Len(t, finalizeResp.TxResults, 3)
	for i, result := range finalizeResp.TxResults {
		require.Equal(t, abci.CodeTypeOK, result.Code, "tx %d: %s", i, result.Log)
	}
	require.Equal(t, finalizeResp.TxResults[1].GasUsed, finalizeResp.TxResults[2].GasUsed,
		"cached and uncached signature verification must consume identical gas")
}

// TestPayForFibreFinalizeBlockColdCacheWithPrunedHistory replays a committed
// PFF tx on a node whose signature cache is cold (as after state sync or a
// restart) once the promise height's historical info has been pruned. The
// result must match the live network, which settled the tx with a warm cache.
func TestPayForFibreFinalizeBlockColdCacheWithPrunedHistory(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), pffTestEscrowAmount)

	// Keep only the two most recent historical entries so the promise height
	// (1) gets pruned while still inside PaymentPromiseHeightWindow.
	setHistoricalEntries(t, testApp, 2)
	for testApp.LastBlockHeight() < 5 {
		execBlock(t, testApp, func(sdk.Context) {})
	}
	_, err := testApp.StakingKeeper.GetHistoricalInfo(uncachedContext(testApp), 1)
	require.Error(t, err, "promise height must be pruned for this test")

	// The tx never went through CheckTx or ProcessProposal on this node, so
	// the signature cache is cold.
	pffTx := newSignedPayForFibreTx(t, signer, accounts[0], true)
	resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   time.Now(),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    [][]byte{pffTx},
	})
	require.NoError(t, err)
	require.Len(t, resp.TxResults, 1)
	require.Equal(t, abci.CodeTypeOK, resp.TxResults[0].Code, resp.TxResults[0].Log)
}

func uncachedContext(testApp *app.App) sdk.Context {
	return testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	})
}

func setHistoricalEntries(t *testing.T, testApp *app.App, entries uint32) {
	t.Helper()
	ctx := uncachedContext(testApp)
	params, err := testApp.StakingKeeper.GetParams(ctx)
	require.NoError(t, err)
	params.HistoricalEntries = entries
	require.NoError(t, testApp.StakingKeeper.SetParams(ctx, params))
}

// buildGasParityTxs returns a MsgSend tx and two same-sized MsgPayForFibre txs.
func buildGasParityTxs(t *testing.T, testApp *app.App, kr keyring.Keyring, newSigner func(int) *user.Signer, accounts []string) ([]byte, [][]byte) {
	t.Helper()
	require.Len(t, accounts, 3)

	addr0 := testfactory.GetAddress(kr, accounts[0])
	sendMsg := banktypes.NewMsgSend(addr0, addr0, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	sendTx, _, err := newSigner(0).CreateTx([]sdk.Msg{sendMsg}, user.SetGasLimit(200_000), user.SetFee(1_000_000))
	require.NoError(t, err)

	pffTxs := make([][]byte, 2)
	for i := range pffTxs {
		account := accounts[i+1]
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), pffTestEscrowAmount)
		pffTxs[i] = newSignedPayForFibreTxWithOpts(t, newSigner(i+1), account, true,
			user.SetGasLimit(1_000_000), user.SetFee(10_000))
	}
	require.Equal(t, len(pffTxs[0]), len(pffTxs[1]), "PayForFibre txs must be identically sized for gas parity")
	return sendTx, pffTxs
}
