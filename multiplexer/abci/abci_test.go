//go:build multiplexer

package abci

import (
	"context"
	"io"
	"os"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/multiplexer/appd"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	abci "github.com/cometbft/cometbft/abci/types"
	db "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/require"
)

func TestOfferSnapshot(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "multiplexer-test-*")
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(tempDir)
		require.NoError(t, err)
	}()

	serverContext := server.NewDefaultContext()
	serverContext.Config.SetRoot(tempDir)
	serverConfig := serverconfig.Config{}
	clientContext := client.Context{}
	appCreator := mockAppCreator()
	versions := getVersions(t)
	chainId := appconsts.TestChainID
	applicationVersion := uint64(3)

	multiplexer, err := NewMultiplexer(serverContext, serverConfig, clientContext, appCreator, versions, chainId, applicationVersion)
	require.NoError(t, err)

	t.Run("should return an error if the app version in the snapshot is not supported", func(t *testing.T) {
		_, err := multiplexer.OfferSnapshot(context.Background(), &abci.RequestOfferSnapshot{
			Snapshot: &abci.Snapshot{
				Height:   1,
				Format:   1,
				Chunks:   1,
				Hash:     []byte("test"),
				Metadata: []byte("test"),
			},
			AppHash:    []byte("test"),
			AppVersion: 100,
		})
		require.Error(t, err)

		// Note this error message is not ideal. The multiplexer should not try to enable the GRPC and API servers for an unsupported version.
		require.ErrorContains(t, err, "failed to get app for version 100: failed to enable gRPC and API servers: unable to enable grpc and api servers, app is nil")
	})
}

func mockAppCreator() servertypes.AppCreator {
	return func(logger log.Logger, db db.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
		return nil
	}
}

func getVersions(t *testing.T) Versions {
	mockAppd := &appd.Appd{}
	versions, err := NewVersions(Version{
		Appd:        mockAppd,
		ABCIVersion: ABCIClientVersion1,
		AppVersion:  3,
		StartArgs:   []string{},
	})
	require.NoError(t, err)
	return versions
}
