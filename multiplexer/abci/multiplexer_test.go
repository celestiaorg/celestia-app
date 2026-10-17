package abci

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/multiplexer/appd"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/stretchr/testify/require"
)

// TestInitTelemetryIsIdempotent verifies that initTelemetry is idempotent.
//
// When telemetry.enabled=true with prometheus-retention-time > 0,
// telemetry.New() registers a PrometheusSink on the global
// prometheus.DefaultRegisterer. In multiplexer mode,
// enableGRPCAndAPIServers may be called more than once (e.g. during a
// version switch). Without caching, the second telemetry.New() call fails
// with "duplicate metrics collector registration attempted".
//
// initTelemetry must be idempotent: the second call should be a no-op.
// Removing the caching guard causes this test to fail.
func TestInitTelemetryIsIdempotent(t *testing.T) {
	m := &Multiplexer{
		svrCfg: serverconfig.Config{
			Telemetry: telemetry.Config{
				Enabled:                 true,
				ServiceName:             "test",
				PrometheusRetentionTime: 60, // positive value enables Prometheus sink
			},
		},
	}

	// First call registers a PrometheusSink on the global
	// prometheus.DefaultRegisterer.
	require.NoError(t, m.initTelemetry())
	require.NotNil(t, m.metrics)

	// Second call must succeed. Without the caching guard this fails with
	// "duplicate metrics collector registration attempted".
	require.NoError(t, m.initTelemetry())
}

func TestOpenTraceWriter(t *testing.T) {
	t.Run("openTraceWriter with empty file does not error", func(t *testing.T) {
		_, err := openTraceWriter("")
		require.NoError(t, err)
	})
}

// commitApp is a native app whose FinalizeBlock reports no consensus param updates.
type commitApp struct {
	servertypes.Application
}

func (commitApp) FinalizeBlock(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return &abci.ResponseFinalizeBlock{}, nil
}

func (commitApp) Commit() (*abci.ResponseCommit, error) {
	return &abci.ResponseCommit{}, nil
}

func (commitApp) OfferSnapshot(*abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_ACCEPT}, nil
}

// TestCommitWithoutVersionUpdateKeepsAppVersion verifies that Commit keeps the
// current app version when FinalizeBlock does not report one.
func TestCommitWithoutVersionUpdateKeepsAppVersion(t *testing.T) {
	versions, err := NewVersions(Version{Appd: &appd.Appd{}, ABCIVersion: ABCIClientVersion1, AppVersion: 3})
	require.NoError(t, err)
	m, err := NewMultiplexer(server.NewDefaultContext(), serverconfig.Config{}, client.Context{}, nil, versions, "test", 10, nil)
	require.NoError(t, err)
	m.nativeApp = commitApp{}

	_, err = m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	_, err = m.Commit(context.Background(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.Equal(t, uint64(10), m.appVersion)
}

// TestCommitAfterSnapshotKeepsSnapshotAppVersion verifies that the first Commit
// after state sync keeps the app version from the snapshot.
func TestCommitAfterSnapshotKeepsSnapshotAppVersion(t *testing.T) {
	versions, err := NewVersions(Version{Appd: &appd.Appd{}, ABCIVersion: ABCIClientVersion1, AppVersion: 3})
	require.NoError(t, err)
	m, err := NewMultiplexer(server.NewDefaultContext(), serverconfig.Config{}, client.Context{}, nil, versions, "test", 10, nil)
	require.NoError(t, err)
	m.nativeApp = commitApp{}

	_, err = m.OfferSnapshot(context.Background(), &abci.RequestOfferSnapshot{AppVersion: 11})
	require.NoError(t, err)
	_, err = m.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
	require.NoError(t, err)
	_, err = m.Commit(context.Background(), &abci.RequestCommit{})
	require.NoError(t, err)
	require.Equal(t, uint64(11), m.appVersion)
}
