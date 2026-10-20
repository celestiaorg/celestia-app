package app

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildProposalSquareCancelledIsAnError pins that a cancelled build reports
// an error. Its result is otherwise zero, and a zero size and root would match
// a proposal claiming SquareSize 0 with an empty data root.
func TestBuildProposalSquareCancelledIsAnError(t *testing.T) {
	var cancel atomic.Bool
	cancel.Store(true)
	result := (&App{}).buildProposalSquare(nil, nil, nil, 64, 0, &cancel)
	require.ErrorIs(t, result.err, errProposalSquareCancelled)
}
