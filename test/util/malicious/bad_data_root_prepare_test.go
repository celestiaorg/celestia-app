package malicious

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v12/app"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// TestBadDataRootPrepareProposal checks that the honest ProcessProposal rejects
// the proposal built by the bad data root handler.
func TestBadDataRootPrepareProposal(t *testing.T) {
	badApp := NewTestApp(app.DefaultConsensusParams(), BehaviorConfig{HandlerName: BadDataRootHandlerKey, StartHeight: 1})
	req := &abci.RequestPrepareProposal{Height: badApp.LastBlockHeight() + 1, Time: time.Now()}

	honest, err := badApp.App.PrepareProposal(req)
	require.NoError(t, err)
	bad, err := badApp.PrepareProposal(req)
	require.NoError(t, err)
	require.Equal(t, honest.Txs, bad.Txs)
	require.NotEqual(t, honest.DataRootHash, bad.DataRootHash)

	resp, err := badApp.App.ProcessProposal(&abci.RequestProcessProposal{
		Height:       req.Height,
		Time:         req.Time,
		Txs:          bad.Txs,
		DataRootHash: bad.DataRootHash,
		SquareSize:   bad.SquareSize,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
}
