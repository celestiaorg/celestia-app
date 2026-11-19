package abci

import (
	"context"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/proxy"
	"github.com/stretchr/testify/require"
)

// recheckTxsApp is a native app that takes batch rechecks.
type recheckTxsApp struct {
	*fakeApp
	batches int
}

func (a *recheckTxsApp) RecheckTxs(_ context.Context, req *abci.RequestRecheckTxs) (*abci.ResponseRecheckTxs, error) {
	a.batches++
	return &abci.ResponseRecheckTxs{Responses: make([]*abci.ResponseCheckTx, len(req.Txs))}, nil
}

// TestRecheckTxsFollowsUpgradeToNativeApp checks, through the mempool connection core uses,
// that a batch recheck is unsupported while the embedded v10 app serves (core then rechecks
// per tx) and reaches the native app once the upgrade block commits, without a restart.
func TestRecheckTxsFollowsUpgradeToNativeApp(t *testing.T) {
	native := &recheckTxsApp{fakeApp: &fakeApp{}}
	m := newV10Multiplexer(t, &v10UpgradeServer{}, native)
	cli, err := proxy.NewConnSyncLocalClientCreator(m).NewABCIClient()
	require.NoError(t, err)
	mempool, ok := proxy.NewAppConnMempool(cli, proxy.NopMetrics()).(abci.RecheckTxsApplication)
	require.True(t, ok)
	req := &abci.RequestRecheckTxs{Txs: [][]byte{{1}, {2}}}

	_, err = mempool.RecheckTxs(t.Context(), req)
	require.ErrorIs(t, err, abci.ErrRecheckTxsUnsupported)

	// The upgrade block runs on v10, which stays active until it commits.
	_, err = m.FinalizeBlock(t.Context(), &abci.RequestFinalizeBlock{Height: 100})
	require.NoError(t, err)
	_, err = mempool.RecheckTxs(t.Context(), req)
	require.ErrorIs(t, err, abci.ErrRecheckTxsUnsupported)
	require.Zero(t, native.batches)

	_, err = m.Commit(t.Context(), &abci.RequestCommit{})
	require.NoError(t, err)
	rsp, err := mempool.RecheckTxs(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, rsp.Responses, len(req.Txs))
	require.Equal(t, 1, native.batches)
}

// TestRecheckTxsNativeAppWithoutBatch checks that a native app without RecheckTxs is reported
// as unsupported rather than served.
func TestRecheckTxsNativeAppWithoutBatch(t *testing.T) {
	m := newActivationTestMultiplexer(&fakeApp{}, 3)
	_, err := m.RecheckTxs(t.Context(), &abci.RequestRecheckTxs{Txs: [][]byte{{1}}})
	require.ErrorIs(t, err, abci.ErrRecheckTxsUnsupported)
}
