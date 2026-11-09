package docker_e2e

import (
	"testing"

	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/p2p"
	"github.com/stretchr/testify/require"
)

// TestGenesisSyncNodeKeyID checks that the embedded node key still has the ID
// that Celestia's archive nodes reserve a slot for.
func TestGenesisSyncNodeKeyID(t *testing.T) {
	var key p2p.NodeKey
	require.NoError(t, cmtjson.Unmarshal(genesisSyncNodeKey, &key))
	require.Equal(t, genesisSyncNodeID, string(key.ID()))
}
