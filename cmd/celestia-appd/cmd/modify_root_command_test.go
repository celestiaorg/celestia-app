//go:build !multiplexer

package cmd

import (
	"reflect"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/stretchr/testify/require"
)

// The standalone start command hands the app the node's mempool before the
// node starts, as the multiplexer does.
func TestStandaloneStartWiresPendingSequencer(t *testing.T) {
	opts := standaloneStartCmdOptions()
	require.NotNil(t, opts.PreNodeStart)
	require.Equal(t, reflect.ValueOf(app.WirePendingSequencer).Pointer(), reflect.ValueOf(opts.PreNodeStart).Pointer())
}
