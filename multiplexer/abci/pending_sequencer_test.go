package abci

import (
	"testing"

	"github.com/cometbft/cometbft/mempool"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/require"
)

type sequencerApp struct {
	servertypes.Application
	got mempool.PendingSequencer
}

func (a *sequencerApp) SetPendingSequencer(p mempool.PendingSequencer) { a.got = p }

type fakeSequencer struct{}

func (fakeSequencer) HighestPendingSequence([]byte) (uint64, bool) { return 0, false }

func TestWirePendingSequencer(t *testing.T) {
	app := &sequencerApp{}
	wirePendingSequencer(app, nil)
	require.Nil(t, app.got)
	wirePendingSequencer(app, fakeSequencer{})
	require.Equal(t, fakeSequencer{}, app.got)

	// Apps without the setter, and no app at all, are left alone.
	require.NotPanics(t, func() {
		wirePendingSequencer(nil, fakeSequencer{})
		wirePendingSequencer(struct{ servertypes.Application }{}, fakeSequencer{})
	})
}
