package app_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/stretchr/testify/require"
)

func TestDynamicTimeouts(t *testing.T) {
	const (
		val1 = "validator1"
		val2 = "validator2"
	)

	cfg1 := testnode.DefaultConfig().
		WithGenesis(genesis.NewDefaultGenesis().
			WithValidators(genesis.NewDefaultValidator(val1), genesis.NewDefaultValidator(val2)))

	cfg2 := testnode.DefaultConfig().WithGenesis(cfg1.Genesis).WithSuppressLogs(false)

	ctx1, _, _ := testnode.NewNetwork(t, cfg1)
	res, err := ctx1.Client.Status(ctx1.GoContext())
	require.NoError(t, err)

	peerID := fmt.Sprintf("%s@%s", res.NodeInfo.DefaultNodeID, strings.TrimPrefix(res.NodeInfo.ListenAddr, "tcp://"))
	cfg2.TmConfig.P2P.PersistentPeers = peerID

	ctx2, _, _ := testnode.NewNetwork(t, cfg2, 1)

	_, err = ctx2.WaitForHeight(2)
	require.NoError(t, err)
}
