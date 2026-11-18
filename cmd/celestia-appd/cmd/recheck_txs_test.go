package cmd

import (
	"testing"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/proxy"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

// TestRecheckTxsReachesAppInStandaloneBuild builds the mempool connection as the SDK start
// command does, the app in cometABCIWrapper, with each in-process client creator, and checks
// that a batch recheck reaches the app instead of falling back to per-tx CheckTx.
func TestRecheckTxsReachesAppInStandaloneBuild(t *testing.T) {
	opts := simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir(), flags.FlagChainID: "test", server.FlagPruning: "nothing"}
	wrapped := server.NewCometABCIWrapper(NewAppServer(log.NewNopLogger(), dbm.NewMemDB(), nil, opts))
	req := &abci.RequestRecheckTxs{Txs: [][]byte{{1}, {2}}}
	for name, cc := range map[string]proxy.ClientCreator{
		"local":     proxy.NewLocalClientCreator(wrapped),
		"conn-sync": proxy.NewConnSyncLocalClientCreator(wrapped),
		// control: a wrapper without the method is reported, not silently served
		"hidden": proxy.NewLocalClientCreator(struct{ abci.Application }{wrapped}),
	} {
		cli, err := cc.NewABCIClient()
		require.NoError(t, err)
		conn, ok := proxy.NewAppConnMempool(cli, proxy.NopMetrics()).(abci.RecheckTxsApplication)
		require.True(t, ok)
		rsp, err := conn.RecheckTxs(t.Context(), req)
		if name == "hidden" {
			require.ErrorIs(t, err, abci.ErrRecheckTxsUnsupported)
			continue
		}
		require.NoError(t, err, name)
		require.Len(t, rsp.Responses, len(req.Txs), name)
	}
}
