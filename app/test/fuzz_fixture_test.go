package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFuzzFixturesDeterministic checks that the fuzz fixtures sign the same
// bytes every time they are built, so a saved crash input replays against the
// txs it was found on and minimization compares like with like. A fixture that
// pulled keys or data from crypto/rand would differ between the two builds.
func TestFuzzFixturesDeterministic(t *testing.T) {
	t.Run("checkTx", func(t *testing.T) {
		first, second := newCheckTxFixture(t), newCheckTxFixture(t)
		t.Cleanup(func() {
			require.NoError(t, first.app.Close())
			require.NoError(t, second.app.Close())
		})
		require.Equal(t, first.txs, second.txs)
	})
	t.Run("proposal", func(t *testing.T) {
		first, second := newProposalFixture(t), newProposalFixture(t)
		t.Cleanup(func() {
			require.NoError(t, first.app.Close())
			require.NoError(t, second.app.Close())
		})
		require.Equal(t, first.mempool, second.mempool)
		require.Equal(t, first.txs, second.txs)
		require.Equal(t, first.dataRoot, second.dataRoot)
	})
}
