package e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

// TestFullLongBehaviour mainly lets a multiple validator network run for 100 blocks, then checks if
// the valsets and data commitments are relayed correctly.
// currently, it takes around 10min to reach 120 block.
func TestFullLongBehaviour(t *testing.T) {
	if os.Getenv("QGB_FULL_INTEGRATION_TEST") != "true" {
		t.Skip("Skipping QGB full integration tests")
	}

	network, err := NewQGBNetwork()
	HandleNetworkError(t, network, err)

	// preferably, run this also when ctrl+c
	defer network.DeleteAll() //nolint:errcheck

	// start full network with four validatorS
	err = network.StartAll()
	HandleNetworkError(t, network, err)

	ctx := context.TODO()
	err = network.WaitForBlock(ctx, 120)
	HandleNetworkError(t, network, err)

	// check whether the four validators are up and running
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err)

	// check whether all the validators are up and running
	lastValsets, err := querier.QueryLastValsets(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(lastValsets[0].Members))

	// check whether the QGB contract was deployed
	bridge, err := network.GetLatestDeployedQGBContract(ctx)
	HandleNetworkError(t, network, err)

	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	// check whether the relayer relayed all data commitments
	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, dcNonce, 100/types.DataCommitmentWindow)

	// check whether the relayer relayed all valsets
	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, vsNonce, lastValsets[0].Nonce)
}
