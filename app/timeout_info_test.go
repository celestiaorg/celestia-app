package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v9/app"
	consensustimeoutstypes "github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
)

// newAppForTimeoutInfoTest builds a fresh app with the supplied chain-id and
// CLI override timeouts. We use the same Noop helpers as the other app_test
// fixtures (NoopWriter / NoopAppOptions). The returned app has not had
// InitChain called, but the ConsensusTimeoutsKeeper falls back to
// DefaultParams() when no row is set, which is enough for TimeoutInfo to be
// exercised against any sdk.Context with a mounted store.
func newAppForTimeoutInfoTest(
	t *testing.T,
	chainID string,
	timeoutCommit time.Duration,
	delayedPrecommitTimeout time.Duration,
) *app.App {
	t.Helper()
	logger := log.NewNopLogger()
	db := tmdb.NewMemDB()
	traceStore := &NoopWriter{}
	appOptions := NoopAppOptions{}

	return app.New(
		logger,
		db,
		traceStore,
		delayedPrecommitTimeout,
		timeoutCommit,
		appOptions,
		baseapp.SetChainID(chainID),
	)
}

// TestTimeoutInfo_KeeperValueIsReturned asserts that when no CLI overrides
// apply (or the chain-id permits them but the test does not exercise them),
// TimeoutInfo reflects exactly the keeper-stored params for all 8 fields.
func TestTimeoutInfo_KeeperValueIsReturned(t *testing.T) {
	// Construct app with no overrides (timeoutCommit=0, delayedPrecommitTimeout=0).
	a := newAppForTimeoutInfoTest(t, "test", 0, 0)

	// Build a context against the app's commit store so the consensustimeouts
	// keeper can read/write its params row.
	ctx := a.NewUncachedContext(false, cmtproto.Header{})

	want := consensustimeoutstypes.NewParams(
		1500*time.Millisecond, // TimeoutPropose
		250*time.Millisecond,  // TimeoutProposeDelta
		1500*time.Millisecond, // TimeoutPrevote
		250*time.Millisecond,  // TimeoutPrevoteDelta
		1500*time.Millisecond, // TimeoutPrecommit
		250*time.Millisecond,  // TimeoutPrecommitDelta
		300*time.Millisecond,  // TimeoutCommit
		1100*time.Millisecond, // DelayedPrecommitTimeout
	)
	require.NoError(t, want.Validate(), "test params should validate")

	a.ConsensusTimeoutsKeeper.SetParams(ctx, want)

	got := a.TimeoutInfo(ctx)
	require.Equal(t, want.TimeoutPropose, got.TimeoutPropose)
	require.Equal(t, want.TimeoutProposeDelta, got.TimeoutProposeDelta)
	require.Equal(t, want.TimeoutPrevote, got.TimeoutPrevote)
	require.Equal(t, want.TimeoutPrevoteDelta, got.TimeoutPrevoteDelta)
	require.Equal(t, want.TimeoutPrecommit, got.TimeoutPrecommit)
	require.Equal(t, want.TimeoutPrecommitDelta, got.TimeoutPrecommitDelta)
	require.Equal(t, want.TimeoutCommit, got.TimeoutCommit)
	require.Equal(t, want.DelayedPrecommitTimeout, got.DelayedPrecommitTimeout)
}

// TestTimeoutInfo_CLIOverrideByChainID exercises the override-vs-ignore matrix:
// non-production chain-ids ("test") honour the CLI overrides, while the three
// production chain-ids ignore them and return the keeper's value (the default
// in this test, since SetParams is not called).
func TestTimeoutInfo_CLIOverrideByChainID(t *testing.T) {
	const (
		cliTimeoutCommit           = 100 * time.Millisecond
		cliDelayedPrecommitTimeout = 900 * time.Millisecond
	)

	cases := []struct {
		chainID         string
		expectOverrides bool
	}{
		{chainID: "test", expectOverrides: true},
		{chainID: "celestia", expectOverrides: false},   // mainnet
		{chainID: "mocha-4", expectOverrides: false},    // testnet
		{chainID: "arabica-11", expectOverrides: false}, // devnet
	}

	defaults := consensustimeoutstypes.DefaultParams()

	for _, tc := range cases {
		t.Run(tc.chainID, func(t *testing.T) {
			a := newAppForTimeoutInfoTest(t, tc.chainID, cliTimeoutCommit, cliDelayedPrecommitTimeout)
			ctx := a.NewUncachedContext(false, cmtproto.Header{})

			info := a.TimeoutInfo(ctx)

			// Untouched fields always come from the keeper (defaults here).
			require.Equal(t, defaults.TimeoutPropose, info.TimeoutPropose)
			require.Equal(t, defaults.TimeoutProposeDelta, info.TimeoutProposeDelta)
			require.Equal(t, defaults.TimeoutPrevote, info.TimeoutPrevote)
			require.Equal(t, defaults.TimeoutPrevoteDelta, info.TimeoutPrevoteDelta)
			require.Equal(t, defaults.TimeoutPrecommit, info.TimeoutPrecommit)
			require.Equal(t, defaults.TimeoutPrecommitDelta, info.TimeoutPrecommitDelta)

			if tc.expectOverrides {
				require.Equal(t, cliTimeoutCommit, info.TimeoutCommit,
					"non-production chain-id %q should honour the CLI TimeoutCommit override", tc.chainID)
				require.Equal(t, cliDelayedPrecommitTimeout, info.DelayedPrecommitTimeout,
					"non-production chain-id %q should honour the CLI DelayedPrecommitTimeout override", tc.chainID)
			} else {
				require.Equal(t, defaults.TimeoutCommit, info.TimeoutCommit,
					"production chain-id %q must ignore the CLI TimeoutCommit override", tc.chainID)
				require.Equal(t, defaults.DelayedPrecommitTimeout, info.DelayedPrecommitTimeout,
					"production chain-id %q must ignore the CLI DelayedPrecommitTimeout override", tc.chainID)
			}
		})
	}
}
