package ante

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPendingTxSigReraisesVerifierPanic: a panicking verify must surface on the
// ante goroutine, as it would in line, and must count as failed.
func TestPendingTxSigReraisesVerifierPanic(t *testing.T) {
	p := &pendingTxSig{}
	p.start(7, 1, "chain", func() error { panic("verify panicked") })
	require.True(t, p.failed())
	require.PanicsWithValue(t, "verify panicked", func() { _ = p.wait() })
}
