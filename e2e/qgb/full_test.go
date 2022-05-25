package e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
)

// TestFullLongBehaviour mainly lets a multiple validator network run for 100 blocks, then checks if
// the valsets and data commitments are relayed correctly.
// currently, it takes around 10min to reach 120 block.
func TestFullLongBehaviour(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	HandleNetworkError(t, network, err, false)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	// start full network with four validatorS
	err = network.StartAll()
	HandleNetworkError(t, network, err, false)

	err = network.WaitForBlockWithCustomTimeout(network.Context, 120, 8*time.Minute)
	HandleNetworkError(t, network, err, false)

	// check whether the four validators are up and running
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err, false)

	// check whether all the validators are up and running
	lastValsets, err := querier.QueryLastValsets(network.Context)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(lastValsets[0].Members))

	// check whether the QGB contract was deployed
	bridge, err := network.GetLatestDeployedQGBContract(network.Context)
	HandleNetworkError(t, network, err, false)

	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	// check whether the relayer relayed all data commitments
	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: network.Context})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, dcNonce, 100/types.DataCommitmentWindow)

	// check whether the relayer relayed all valsets
	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: network.Context})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, vsNonce, lastValsets[0].Nonce)
}
