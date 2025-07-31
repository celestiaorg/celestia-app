package interop

import (
	"encoding/json"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	ibctesting "github.com/cosmos/ibc-go/v8/testing"
	"github.com/stretchr/testify/require"
)

// SetupTestingApp returns a simapp instance that has PFM wired up
func SetupTestingApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	db := dbm.NewMemDB()
	app := NewSimApp(log.NewNopLogger(), db, nil, true, simtestutil.EmptyAppOptions{})
	return app, app.DefaultGenesis()
}

func SetupTest(t *testing.T) (*ibctesting.Coordinator, *ibctesting.TestChain,
	*ibctesting.TestChain, *ibctesting.TestChain,
) {
	chains := make(map[string]*ibctesting.TestChain)
	coordinator := &ibctesting.Coordinator{
		T:           t,
		CurrentTime: time.Now(),
		Chains:      chains,
	}

	// modify ibctesting package to return celestia as the next app when calling ibctesting.NewTestChain
	ibctesting.DefaultTestingAppInit = func() (ibctesting.TestingApp, map[string]json.RawMessage) {
		db := dbm.NewMemDB()
		celestiaApp := app.New(log.NewNopLogger(), db, nil, 0, simtestutil.EmptyAppOptions{})
		return celestiaApp, celestiaApp.DefaultGenesis()
	}

	celestiaChain := ibctesting.NewTestChain(t, coordinator, ibctesting.GetChainID(1))
	setMinFeeToZero(t, celestiaChain)

	// modify the testing package to return a pfm app
	ibctesting.DefaultTestingAppInit = SetupTestingApp

	chainA := ibctesting.NewTestChain(t, coordinator, ibctesting.GetChainID(2))
	chainB := ibctesting.NewTestChain(t, coordinator, ibctesting.GetChainID(3))

	coordinator.Chains[ibctesting.GetChainID(1)] = celestiaChain
	coordinator.Chains[ibctesting.GetChainID(2)] = chainA
	coordinator.Chains[ibctesting.GetChainID(3)] = chainB
	return coordinator, celestiaChain, chainA, chainB
}

// setMinFeeToZero updates the network minimum gas price to zero.
// This is a workaround as overriding at genesis will fail in minfee.ValidateGenesis
func setMinFeeToZero(t *testing.T, celestiaChain *ibctesting.TestChain) {
	celestiaApp, ok := celestiaChain.App.(*app.App)
	require.True(t, ok)

	params := celestiaApp.MinFeeKeeper.GetParams(celestiaChain.GetContext())
	params.NetworkMinGasPrice = math.LegacyNewDec(0)
	celestiaApp.MinFeeKeeper.SetParams(celestiaChain.GetContext(), params)
}
