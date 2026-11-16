package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestCheckTxAfterRestart checks that a node restarted on committed state
// accepts a correctly signed tx before it applies its next block.
func TestCheckTxAfterRestart(t *testing.T) {
	accounts := testfactory.GenerateAccounts(3)
	db := dbm.NewMemDB()
	a := testutil.NewTestAppWithDB(db)
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(a, accounts...)
	a = testutil.InitialiseTestAppWithGenesis(a, app.DefaultConsensusParams(), genesis)
	for h := int64(1); h <= 3; h++ {
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time: testutil.GenesisTime.Add(time.Duration(h) * time.Second), Height: h,
			Hash: a.LastCommitID().Hash, NextValidatorsHash: valSet.Hash(),
		})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
	}

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	infos := queryAccountInfo(a, accounts, kr)
	idx := len(accounts) - 1
	require.NotZero(t, infos[idx].AccountNum)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(idx)
	from := signer.Account(accounts[idx]).Address()
	msg := banktypes.NewMsgSend(from, from, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	tx, _, err := signer.CreateTx([]sdk.Msg{msg}, blobfactory.DefaultSDKTxOpts()...)
	require.NoError(t, err)

	res, err := a.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code, "before restart: %s", res.Log)

	// Restart: a new app on the same DB, at height 3, no block applied yet.
	restarted := testutil.NewTestAppWithDB(db)
	require.EqualValues(t, 3, restarted.LastBlockHeight())
	res, err = restarted.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code, "after restart: %s", res.Log)
}

// TestQueryBlockTimeAfterRestart checks that queries served after a restart,
// before the next block, see the last block time rather than the zero time.
func TestQueryBlockTimeAfterRestart(t *testing.T) {
	db := dbm.NewMemDB()
	a := testutil.NewTestAppWithDB(db)
	genesis, valSet, _ := testutil.GenesisStateWithSingleValidator(a, testfactory.GenerateAccounts(1)...)
	a = testutil.InitialiseTestAppWithGenesis(a, app.DefaultConsensusParams(), genesis)
	last := testutil.GenesisTime.Add(time.Minute)
	_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Time: last, Height: 1, Hash: a.LastCommitID().Hash, NextValidatorsHash: valSet.Hash()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)

	restarted := testutil.NewTestAppWithDB(db)
	ctx, err := restarted.CreateQueryContext(0, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, ctx.BlockHeight())
	require.True(t, last.Equal(ctx.BlockTime()), "query block time %v, want %v", ctx.BlockTime(), last)
}
