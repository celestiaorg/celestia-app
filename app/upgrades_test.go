package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
)

func TestRegisterUpgradeHandlers(t *testing.T) {
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	timeoutCommit := time.Second
	appOptions := NoopAppOptions{}

	// app.New() should invoke RegisterUpgradeHandlers.
	testApp := app.New(logger, db, traceStore, timeoutCommit, appOptions, baseapp.SetChainID(testfactory.ChainID))

	require.False(t, testApp.UpgradeKeeper.HasHandler("v5"))
	require.True(t, testApp.UpgradeKeeper.HasHandler("v6"))
}
