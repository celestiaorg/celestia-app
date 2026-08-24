package hardspoon

import (
	"os"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v9/app"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/stretchr/testify/require"
)

// realExportEnv points at a genuine celestia-appd export. Such a file is far too
// large to commit, so these tests skip unless it is set:
//
//	HARDSPOON_EXPORT=/path/to/fork.json go test ./app/hardspoon/ -run RealExport -v
const realExportEnv = "HARDSPOON_EXPORT"

func loadRealExport(t *testing.T) (*Fork, *app.App) {
	t.Helper()

	path := os.Getenv(realExportEnv)
	if path == "" {
		t.Skipf("set %s to a celestia-appd export to run this", realExportEnv)
	}

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	fork, err := LoadFork(raw)
	require.NoError(t, err)

	capp := app.New(
		log.NewNopLogger(), dbm.NewMemDB(), nil, 0, 0,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)
	return fork, capp
}

// TestRealExportEscrowDerivation checks the escrow derivation against a real
// chain's channel list.
//
// Dropping the IBC escrow balances is the one step that deletes funds from
// addresses that are not labelled as anything special: they are ordinary
// accounts, and hardspoon only knows which ones they are by re-deriving them
// from the channel list. transfer.total_escrowed is maintained independently by
// the transfer module, so requiring the two to agree is what stands between a
// derivation bug and silently deleted user balances.
//
// This is the assertion worth running against real data, because a fixture with
// a single channel cannot show that the derivation holds across hundreds.
func TestRealExportEscrowDerivation(t *testing.T) {
	fork, capp := loadRealExport(t)

	s := &spoon{cdc: capp.AppCodec(), fork: fork, report: &Report{}}
	require.NoError(t, s.load())

	addresses, err := s.escrowAddresses()
	require.NoError(t, err, "escrow derivation must reconcile with transfer.total_escrowed")

	t.Logf(
		"derived %d transfer escrow addresses from %d channels, holding %s",
		len(addresses), len(s.channels), s.report.EscrowHeld,
	)
	require.Equal(t, s.transfer.TotalEscrowed.String(), s.report.EscrowHeld.String())
}

// TestRealExportTransform runs the whole transform against a real export.
//
// It doubles as a check on which kind of export was supplied. A plain export is
// rejected up front, and the assertion below pins down that the refusal explains
// itself, because that refusal is all that stands between a stale starting stake
// and over-credited delegators. Point the env var at a --for-zero-height export
// and the same test becomes the real-data end-to-end run, reporting the size that
// decides whether the genesis fits under the ABCI receive limit.
func TestRealExportTransform(t *testing.T) {
	fork, capp := loadRealExport(t)

	opts := Options{
		ChainID:       "mocha-5",
		GenesisTime:   time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC),
		InitialHeight: 1,
		AppVersion:    9,
	}

	result, err := Transform(capp.AppCodec(), capp.DefaultGenesis(), fork, opts)
	if err != nil {
		require.Contains(t, err.Error(), "--for-zero-height",
			"a plain export must be refused with an explanation, got: %v", err)
		t.Logf("refused, as expected for a plain export:\n%v", err)
		return
	}

	t.Logf("%s", result.Report)
	require.LessOrEqual(t, result.Report.SizeBytes, DefaultMaxSizeBytes)

	// mocha-4 carries synthetic warp coins and an IBC voucher, and neither kind
	// survives the spoon in redeemable form. None may reach the new chain, and
	// every denom the old supply held has to be accounted for in the report
	// rather than quietly vanish.
	orphaned := 0
	for _, coin := range result.Report.In.Supply {
		if orphanedDenom(coin.Denom) {
			orphaned++
		}
	}
	require.Len(t, result.Report.OrphanedDenoms, orphaned)
	for _, coin := range result.Report.Out.Supply {
		require.False(t, orphanedDenom(coin.Denom), "supply still carries %s", coin)
	}
}
