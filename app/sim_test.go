//go:build sims

package app_test

import (
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobsim "github.com/celestiaorg/celestia-app/v11/x/blob/simulation"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	signalsim "github.com/celestiaorg/celestia-app/v11/x/signal/simulation"
	signaltypes "github.com/celestiaorg/celestia-app/v11/x/signal/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	"github.com/stretchr/testify/require"
)

// simChainID is not a chain ID with a short upgrade delay, so a MsgTryUpgrade
// that reaches quorum schedules an upgrade far past the end of the run.
const simChainID = "simulation-app"

// defaultSimSeeds are the seeds TestAppStateDeterminism uses when -Seed is
// not set. They are fixed so that a run can be reproduced. The simulator
// flags (-Seed, -NumBlocks, ...) are registered by the test/util package,
// which this test package imports.
var defaultSimSeeds = []int64{1, 42}

// TestAppStateDeterminism runs the simulation several times with the same
// seed and checks that every run ends with the same app hash.
//
// Run it with `make test-sim-determinism`.
func TestAppStateDeterminism(t *testing.T) {
	const runsPerSeed = 3

	config := simcli.NewConfigFromFlags()
	config.ChainID = simChainID
	config.InitialBlockHeight = 1
	config.ExportParamsPath = ""
	config.OnOperation = false
	config.AllInvariants = false
	// Without commits the app hash never changes.
	config.Commit = true

	seeds := defaultSimSeeds
	if config.Seed != simcli.DefaultSeedValue {
		seeds = []int64{config.Seed}
	}

	// The SDK BaseApp stores InitialAppVersion when InitChain has no app
	// version. The simulation sends random consensus params without one.
	prevInitialAppVersion := baseapp.InitialAppVersion
	baseapp.InitialAppVersion = appconsts.Version
	t.Cleanup(func() { baseapp.InitialAppVersion = prevInitialAppVersion })

	for _, seed := range seeds {
		var first simResult
		for run := range runsPerSeed {
			config.Seed = seed
			res := runSimulation(t, config, fmt.Sprintf("seed=%d/run=%d", seed, run+1))
			t.Logf("seed %d run %d: height %d, app hash %X\n  ops: %s\n  %s",
				seed, run+1, res.height, res.appHash, res.ops, res.finalState)
			res.ops.requireDelivered(t)
			require.Positive(t, res.passedProposals, "no MsgUpdateBlobParams proposal passed")

			if run == 0 {
				first = res
				continue
			}
			require.Equal(t, first.height, res.height, "seed %d: runs 1 and %d stopped at different heights", seed, run+1)
			require.Equal(t, first.appHash, res.appHash,
				"non-determinism in seed %d: runs 1 and %d have different app hashes", seed, run+1)
		}
	}
}

type simResult struct {
	appHash         []byte
	height          int64
	ops             opCounts
	passedProposals int
	finalState      string
}

// runSimulation runs one simulation on a new in-memory app.
//
// SimulateFromSeed calls Skip when every validator is jailed or tombstoned,
// which the random downtime and double-sign evidence of the simulator cause
// within a few hundred blocks because no operation creates validators. It
// runs in a subtest so that such a skip ends only this run. The run then
// stops at the same height every time, so its app hash is still compared.
func runSimulation(t *testing.T, config simtypes.Config, name string) simResult {
	t.Helper()

	logger := log.NewNopLogger()
	if simcli.FlagVerboseValue {
		logger = log.NewTestLogger(t)
	}
	capp := app.New(logger, dbm.NewMemDB(), nil, 0, 0, simtestutil.EmptyAppOptions{},
		baseapp.SetChainID(config.ChainID), writeSimState)

	simManager := module.NewSimulationManagerFromAppModules(capp.ModuleManager.Modules, nil)
	txConfig := capp.GetTxConfig()

	// Only the x/blob and x/signal operations run. The SDK modules' operations
	// are left out: many pay a zero or random fee (x/gov, x/feegrant,
	// x/staking, ...), which the x/minfee network min gas price rejects, and
	// SimulateFromSeed stops the run on any delivery error. The SDK modules
	// still generate the random genesis.
	ops := simulation.WeightedOperations{}
	ops = append(ops, blobsim.WeightedOperations(txConfig, capp.AccountKeeper, capp.BankKeeper, capp.GovKeeper, capp.StakingKeeper)...)
	ops = append(ops, signalsim.WeightedOperations(txConfig, capp.AccountKeeper, capp.BankKeeper, &capp.SignalKeeper, capp.StakingKeeper)...)
	counts := opCounts{}

	t.Run(name, func(t *testing.T) {
		stopEarly, _, err := simulation.SimulateFromSeed(
			t,
			io.Discard,
			capp.BaseApp,
			simtestutil.AppStateFn(capp.AppCodec(), simManager, capp.DefaultGenesis()),
			simtypes.RandomAccounts,
			counts.wrap(ops),
			capp.BlockedAddresses(),
			config,
			capp.AppCodec(),
		)
		require.NoError(t, err)
		require.False(t, stopEarly, "simulation stopped early")
	})
	if t.Failed() {
		t.FailNow()
	}

	res := simResult{appHash: capp.LastCommitID().Hash, height: capp.LastBlockHeight(), ops: counts}
	ctx := capp.NewContextLegacy(true, tmproto.Header{})
	statuses := map[string]int{}
	require.NoError(t, capp.GovKeeper.Proposals.Walk(ctx, nil, func(_ uint64, p govv1.Proposal) (bool, error) {
		statuses[p.Status.String()]++
		return false, nil
	}))
	res.passedProposals = statuses[govv1.StatusPassed.String()]
	res.finalState = fmt.Sprintf("gov proposals by status: %v; blob params: {%v}; signal upgrade pending: %t",
		statuses, capp.BlobKeeper.GetParams(ctx), capp.SignalKeeper.IsUpgradePending(ctx))
	return res
}

// writeSimState makes Commit persist the simulated txs. SimulateFromSeed
// delivers them into the FinalizeBlock state after FinalizeBlock has already
// written that state, so without another write Commit drops them. Upstream
// cosmos-sdk calls BaseApp.SimWriteState for this; the celestia fork has no
// such method, so the precommiter, which Commit calls just before committing,
// does the write.
func writeSimState(bapp *baseapp.BaseApp) {
	bapp.SetPrecommiter(func(ctx sdk.Context) {
		ctx.MultiStore().(storetypes.CacheMultiStore).Write()
	})
}

// opCounts counts the operations of a run by message type: delivered ones
// and no-ops (skipped). A delivery error fails the run, so it is not counted.
type opCounts map[string]*struct{ delivered, skipped int }

// wrap returns ops that count themselves and their future operations.
func (c opCounts) wrap(ops simulation.WeightedOperations) simulation.WeightedOperations {
	wrapped := make(simulation.WeightedOperations, len(ops))
	for i, op := range ops {
		wrapped[i] = simulation.NewWeightedOperation(op.Weight(), c.wrapOp(op.Op()))
	}
	return wrapped
}

func (c opCounts) wrapOp(op simtypes.Operation) simtypes.Operation {
	return func(r *rand.Rand, bapp *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		opMsg, futureOps, err := op(r, bapp, ctx, accs, chainID)
		if c[opMsg.Name] == nil {
			c[opMsg.Name] = &struct{ delivered, skipped int }{}
		}
		if opMsg.OK {
			c[opMsg.Name].delivered++
		} else {
			c[opMsg.Name].skipped++
		}
		for i := range futureOps {
			futureOps[i].Op = c.wrapOp(futureOps[i].Op)
		}
		return opMsg, futureOps, err
	}
}

// requireDelivered fails the test unless every x/blob and x/signal message
// was delivered at least once.
func (c opCounts) requireDelivered(t *testing.T) {
	t.Helper()
	for _, msg := range []sdk.Msg{
		&blobtypes.MsgPayForBlobs{},
		&blobtypes.MsgUpdateBlobParams{},
		&signaltypes.MsgSignalVersion{},
		&signaltypes.MsgTryUpgrade{},
		&govv1.MsgVote{},
	} {
		count := c[sdk.MsgTypeURL(msg)]
		require.True(t, count != nil && count.delivered > 0, "no %s delivered", sdk.MsgTypeURL(msg))
	}
}

func (c opCounts) String() string {
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s delivered=%d skipped=%d", name, c[name].delivered, c[name].skipped))
	}
	return strings.Join(out, ", ")
}
