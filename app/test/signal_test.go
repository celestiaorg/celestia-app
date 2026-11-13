package app_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	signaltypes "github.com/celestiaorg/celestia-app/v11/x/signal/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// TestSignalRejectsNonIncreasingVersionInFinalizeBlock delivers signal
// messages through FinalizeBlock, where the block header carries no app
// version, and checks that the current app version is still enforced.
func TestSignalRejectsNonIncreasingVersionInFinalizeBlock(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	account := accounts[0]
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	accAddr := testfactory.GetAddress(kr, account)
	valAddr := sdk.ValAddress(accAddr)

	newTx := func(msg sdk.Msg) []byte {
		t.Helper()
		tx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimitAndGasPrice(1_000_000, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		require.NoError(t, signer.IncrementSequence(account))
		return tx
	}

	// Create a validator that holds more than 5/6 of the voting power, so its
	// signal alone reaches quorum.
	createValidator, err := stakingtypes.NewMsgCreateValidator(
		valAddr.String(),
		ed25519.GenPrivKey().PubKey(),
		sdk.NewCoin(appconsts.BondDenom, math.NewInt(1_000_000_000_000)),
		stakingtypes.NewDescription("signal", "", "", "", ""),
		stakingtypes.NewCommissionRates(math.LegacyNewDecWithPrec(1, 1), math.LegacyNewDecWithPrec(2, 1), math.LegacyNewDecWithPrec(1, 2)),
		math.OneInt(),
	)
	require.NoError(t, err)
	res := commitBlock(t, testApp, newTx(createValidator))
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code, res.TxResults[0].Log)

	current := appconsts.Version
	res = commitBlock(t, testApp,
		newTx(signaltypes.NewMsgSignalVersion(valAddr.String(), current-1)),
		newTx(signaltypes.NewMsgSignalVersion(valAddr.String(), current)),
		newTx(signaltypes.NewMsgTryUpgrade(accAddr)),
	)
	require.Len(t, res.TxResults, 3)
	require.Equal(t, signaltypes.ErrInvalidSignalVersion.ABCICode(), res.TxResults[0].Code, "signal lower version: %s", res.TxResults[0].Log)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[1].Code, "signal current version: %s", res.TxResults[1].Log)
	require.Equal(t, signaltypes.ErrInvalidUpgradeVersion.ABCICode(), res.TxResults[2].Code, "try upgrade to current version: %s", res.TxResults[2].Log)

	ctx := uncachedContext(testApp)
	require.False(t, testApp.SignalKeeper.IsUpgradePending(ctx))

	// Signalling for a higher version still works.
	res = commitBlock(t, testApp, newTx(signaltypes.NewMsgSignalVersion(valAddr.String(), current+1)))
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code, res.TxResults[0].Log)
}

// TestStalePendingUpgradeIsClearedInFinalizeBlock checks that a pending
// upgrade that does not raise the app version, which older app versions could
// store, is cleared at its upgrade height and no longer blocks signalling.
func TestStalePendingUpgradeIsClearedInFinalizeBlock(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	ctx := uncachedContext(testApp)
	upgrade := signaltypes.Upgrade{AppVersion: appconsts.Version, UpgradeHeight: testApp.LastBlockHeight() + 1}
	bz, err := testApp.AppCodec().Marshal(&upgrade)
	require.NoError(t, err)
	ctx.KVStore(testApp.GetKey(signaltypes.StoreKey)).Set(signaltypes.UpgradeKey, bz)
	require.True(t, testApp.SignalKeeper.IsUpgradePending(ctx))

	commitBlock(t, testApp)

	ctx = uncachedContext(testApp)
	require.False(t, testApp.SignalKeeper.IsUpgradePending(ctx))
	appVersion, err := testApp.AppVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, appconsts.Version, appVersion)
}
