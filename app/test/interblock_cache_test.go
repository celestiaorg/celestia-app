package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	storecache "cosmossdk.io/store/cache"
	"cosmossdk.io/store/iavl"
	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	tmtypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type interBlockCacheAppOptions map[string]any

func (o interBlockCacheAppOptions) Get(key string) any { return o[key] }

func newInterBlockCacheTestApp(mode string) *app.App {
	return app.New(
		testutil.TestAppLogger,
		dbm.NewMemDB(),
		nil,
		0,
		interBlockCacheAppOptions{app.FlagInterBlockCache: mode, app.FlagInterBlockCacheSize: 64},
		baseapp.SetChainID(testutil.ChainID),
	)
}

// newInterBlockCacheTestApps returns an app with the inter-block cache off and
// one in "sync-only" mode, both initialized from the same genesis.
func newInterBlockCacheTestApps(t *testing.T, accounts []string) (off, syncOnly *app.App, valSet *tmtypes.ValidatorSet, kr keyring.Keyring) {
	t.Helper()
	off = newInterBlockCacheTestApp(app.InterBlockCacheOff)
	syncOnly = newInterBlockCacheTestApp(app.InterBlockCacheSyncOnly)
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(off, accounts...)
	testutil.InitialiseTestAppWithGenesis(off, app.DefaultConsensusParams(), genesis)
	testutil.InitialiseTestAppWithGenesis(syncOnly, app.DefaultConsensusParams(), genesis)
	// first block, which commits the genesis state
	for _, a := range []*app.App{off, syncOnly} {
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:               testutil.GenesisTime,
			Height:             1,
			NextValidatorsHash: valSet.Hash(),
		})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, off.LastCommitID(), syncOnly.LastCommitID())
	return off, syncOnly, valSet, kr
}

func TestInterBlockCacheInstalledOnlyInSyncOnly(t *testing.T) {
	off := newInterBlockCacheTestApp(app.InterBlockCacheOff)
	syncOnly := newInterBlockCacheTestApp(app.InterBlockCacheSyncOnly)
	require.False(t, off.InterBlockCacheSyncOnly())
	require.True(t, syncOnly.InterBlockCacheSyncOnly())

	for _, name := range []string{banktypes.StoreKey, "acc", "staking", "distribution"} {
		require.IsType(t, &iavl.Store{}, off.CommitMultiStore().GetKVStore(off.GetKey(name)), name)
		require.IsType(t, &storecache.CommitKVStoreCache{}, syncOnly.CommitMultiStore().GetKVStore(syncOnly.GetKey(name)), name)
	}
}

func TestInterBlockCacheInvalidConfigPanics(t *testing.T) {
	require.Panics(t, func() { newInterBlockCacheTestApp("on") })
}

// TestInterBlockCacheSyncOnlyServesNoQueries checks that an app with the
// inter-block cache on refuses every entry point that could read through the
// cache outside block execution, and that the app with the cache off serves
// them.
func TestInterBlockCacheSyncOnlyServesNoQueries(t *testing.T) {
	accounts := testfactory.GenerateAccounts(2)
	off, syncOnly, _, kr := newInterBlockCacheTestApps(t, accounts)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	tx := testutil.SendTxsWithAccounts(t, off, enc.TxConfig, kr, 1000, accounts[1], accounts[:1], testutil.ChainID)[0]

	// Simulate, before CheckTx moves the check state sequence on.
	_, _, err := off.Simulate(tx)
	require.NoError(t, err)
	_, _, err = syncOnly.Simulate(tx)
	require.ErrorIs(t, err, app.ErrInterBlockCacheSyncOnly)

	// CheckTx, new and recheck.
	checkRes, err := off.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.EqualValues(t, 0, checkRes.Code, checkRes.Log)
	for _, typ := range []abci.CheckTxType{abci.CheckTxType_New, abci.CheckTxType_Recheck} {
		res, err := syncOnly.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: typ})
		require.NoError(t, err)
		require.Equal(t, sdkerrors.ErrInvalidRequest.ABCICode(), res.Code)
		require.Contains(t, res.Log, "sync-only")
	}

	// ABCI queries: gRPC routes, /store, /app/simulate.
	paramsReq, err := (&banktypes.QueryParamsRequest{}).Marshal()
	require.NoError(t, err)
	queries := []*abci.RequestQuery{
		{Path: "/cosmos.bank.v1beta1.Query/Params", Data: paramsReq},
		{Path: "/store/bank/key", Data: []byte{0x01}},
		{Path: "/app/simulate", Data: tx},
	}
	for _, q := range queries {
		res, err := syncOnly.Query(context.Background(), q)
		require.NoError(t, err)
		require.Equal(t, sdkerrors.ErrInvalidRequest.ABCICode(), res.Code, q.Path)
		require.Contains(t, res.Log, "sync-only", q.Path)
	}
	res, err := off.Query(context.Background(), queries[0])
	require.NoError(t, err)
	require.EqualValues(t, 0, res.Code, res.Log)
	// peer filtering reads no state and is still served
	res, err = syncOnly.Query(context.Background(), &abci.RequestQuery{Path: "/p2p/filter/id/abc"})
	require.NoError(t, err)
	require.NotContains(t, res.Log, "sync-only")

	// QuerySequence.
	_, err = off.QuerySequence(context.Background(), &abci.RequestQuerySequence{Signer: testfactory.GetAddress(kr, accounts[0])})
	require.NoError(t, err)
	_, err = syncOnly.QuerySequence(context.Background(), &abci.RequestQuerySequence{Signer: testfactory.GetAddress(kr, accounts[0])})
	require.ErrorIs(t, err, app.ErrInterBlockCacheSyncOnly)

	// State sync.
	offer, err := syncOnly.OfferSnapshot(&abci.RequestOfferSnapshot{})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseOfferSnapshot_REJECT, offer.Result)
	apply, err := syncOnly.ApplySnapshotChunk(&abci.RequestApplySnapshotChunk{})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseApplySnapshotChunk_ABORT, apply.Result)

	// gRPC services, including the tx service with Simulate.
	clientCtx := client.Context{}.WithTxConfig(enc.TxConfig).WithInterfaceRegistry(enc.InterfaceRegistry).WithCodec(enc.Codec)
	for _, a := range []*app.App{off, syncOnly} {
		a.RegisterTxService(clientCtx)
	}
	errDecode := errors.New("decode")
	for _, tc := range []struct {
		app     *app.App
		refuses bool
	}{{off, false}, {syncOnly, true}} {
		capture := &captureServer{}
		tc.app.RegisterGRPCServer(capture)
		require.NotEmpty(t, capture.descs)
		for _, desc := range capture.descs {
			for _, m := range desc.Methods {
				_, err := m.Handler(nil, context.Background(), func(any) error { return errDecode }, nil)
				if tc.refuses {
					require.Equal(t, codes.Unavailable, status.Code(err), desc.ServiceName+"/"+m.MethodName)
				} else {
					require.ErrorIs(t, err, errDecode, desc.ServiceName+"/"+m.MethodName)
				}
			}
		}
	}

	// Info reads committed versions that bypass the cache and is served.
	offInfo, err := off.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	syncInfo, err := syncOnly.Info(&abci.RequestInfo{})
	require.NoError(t, err)
	require.Equal(t, offInfo.LastBlockAppHash, syncInfo.LastBlockAppHash)
}

type captureServer struct{ descs []*grpc.ServiceDesc }

func (c *captureServer) RegisterService(sd *grpc.ServiceDesc, _ any) { c.descs = append(c.descs, sd) }

// TestInterBlockCacheDeterminism runs the same blocks on an app with the
// inter-block cache off and on, and checks that every tx result, event, gas
// value and app hash is identical. The cache is small so that entries are
// evicted and reloaded during the run.
func TestInterBlockCacheDeterminism(t *testing.T) {
	const blocks = 20
	accounts := testfactory.GenerateAccounts(8)
	off, syncOnly, valSet, kr := newInterBlockCacheTestApps(t, accounts)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	for i := 1; i <= blocks; i++ {
		// accounts send to the next one, so balances and sequences of the
		// same accounts are read and written in every block
		var txs [][]byte
		for j, acc := range accounts {
			for _, tx := range testutil.SendTxsWithAccounts(t, off, enc.TxConfig, kr, uint64(1000+i), accounts[(j+1)%len(accounts)], []string{acc}, testutil.ChainID) {
				txs = append(txs, tx)
			}
		}
		req := &abci.RequestFinalizeBlock{
			Time:               testutil.GenesisTime.Add(time.Duration(i) * 6 * time.Second),
			Height:             off.LastBlockHeight() + 1,
			Hash:               off.LastCommitID().Hash,
			NextValidatorsHash: valSet.Hash(),
			ProposerAddress:    valSet.Validators[0].Address,
			Txs:                txs,
		}
		offRes, err := off.FinalizeBlock(req)
		require.NoError(t, err)
		syncRes, err := syncOnly.FinalizeBlock(req)
		require.NoError(t, err)
		for _, r := range offRes.TxResults {
			require.EqualValues(t, 0, r.Code, r.Log)
		}
		require.Equal(t, offRes, syncRes, "block %d", i)

		_, err = off.Commit()
		require.NoError(t, err)
		_, err = syncOnly.Commit()
		require.NoError(t, err)
		require.Equal(t, off.LastCommitID(), syncOnly.LastCommitID(), "block %d", i)
	}
}
