package user_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
)

func TestSigner(t *testing.T) {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)

	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(t, err)
	msg := banktypes.NewMsgSend(
		addr,
		testnode.RandomAddress().(sdk.AccAddress),
		sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)),
	)

	t.Run("should create a tx with a signature that contains a pubkey", func(t *testing.T) {
		tx, err := signer.CreateTx([]sdk.Msg{msg})
		require.NoError(t, err)
		require.NotNil(t, tx)

		decodedTx, err := signer.DecodeTx(tx)
		require.NoError(t, err)

		sigs, err := decodedTx.GetSignaturesV2()
		require.NoError(t, err)
		require.NotNil(t, sigs[0].PubKey)
		require.Equal(t, uint64(0), sigs[0].Sequence)
	})
	t.Run("should create a tx with a signature that contains a pubkey even if sequence is > 0", func(t *testing.T) {
		err := signer.IncrementSequence(account)
		require.NoError(t, err)

		tx, err := signer.CreateTx([]sdk.Msg{msg})
		require.NoError(t, err)
		require.NotNil(t, tx)

		decodedTx, err := signer.DecodeTx(tx)
		require.NoError(t, err)

		sigs, err := decodedTx.GetSignaturesV2()
		require.NoError(t, err)
		require.NotNil(t, sigs[0].PubKey)
		require.Equal(t, uint64(1), sigs[0].Sequence)
	})
}
