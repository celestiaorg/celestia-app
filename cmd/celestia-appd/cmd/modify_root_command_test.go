//go:build !multiplexer

package cmd

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v12/app"
	"github.com/celestiaorg/celestia-app/v12/app/appdb"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// The standalone start command hands the app the node's mempool before the
// node starts, as the multiplexer does.
func TestStandaloneStartWiresPendingSequencer(t *testing.T) {
	opts := standaloneStartCmdOptions()
	require.Equal(t, reflect.ValueOf(app.WirePendingSequencer).Pointer(), reflect.ValueOf(opts.PreNodeStart).Pointer())
	// The hook is set alongside the pebble DB opener, not instead of it.
	require.Equal(t, reflect.ValueOf(appdb.Open).Pointer(), reflect.ValueOf(opts.DBOpener).Pointer())
}

// blockingApp holds FinalizeBlock until release is closed.
type blockingApp struct {
	abci.BaseApplication
	entered, release chan struct{}
}

func (a *blockingApp) FinalizeBlock(context.Context, *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	close(a.entered)
	<-a.release
	return &abci.ResponseFinalizeBlock{}, nil
}

func TestStandaloneCheckTxDoesNotWaitForFinalizeBlock(t *testing.T) {
	blocking := &blockingApp{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(blocking.release)
	creator := standaloneStartCmdOptions().ClientCreator(blocking)
	consensus, err := creator.NewABCIClient()
	require.NoError(t, err)
	mempool, err := creator.NewABCIClient()
	require.NoError(t, err)

	go func() { _, _ = consensus.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{}) }()
	<-blocking.entered

	done := make(chan struct{})
	go func() {
		_, _ = mempool.CheckTx(context.Background(), &abci.RequestCheckTx{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("CheckTx waited for FinalizeBlock")
	}
}
