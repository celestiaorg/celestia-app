package app_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

func TestPayForFibreZeroGasSkipsPreverification(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	raw := newSignedPayForFibreTxWithOpts(t, signer, accounts[0], true,
		user.SetGasLimit(0), user.SetFee(0))
	msg, ok := fibretypes.ParsePayForFibreMsg(raw)
	require.True(t, ok)
	key, err := msg.SigCacheKey()
	require.NoError(t, err)

	observedCache := sigcache.New(1024)
	testApp.FibreKeeper = fibrekeeper.NewKeeper(enc.Codec, testApp.GetKey(fibretypes.StoreKey),
		testApp.BankKeeper, testApp.StakingKeeper, testApp.FibreKeeper.GetAuthority(), false, observedCache)
	response, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, abci.CodeTypeOK, response.Code)
	require.Contains(t, response.Log, "out of gas")
	require.False(t, observedCache.Has(key), "zero-gas rejection must precede certificate verification")
}
