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
