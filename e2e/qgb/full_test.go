package e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

// TestFullLongBehaviour mainly lets a multiple validator network run for 100 blocks, then checks if
// the valsets and data commitments are relayed correctly.
// currently, it takes around 10min to reach 120 block.
func TestFullLongBehaviour(t *testing.T) {
	// Separating it from other e2e as it takes around 20min to finish
	if os.Getenv("QGB_INTEGRATION_FULL_TEST") != TRUE {
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
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil, network.EncCfg)
	HandleNetworkError(t, network, err, false)

	// check whether all the validators are up and running
	latestValset, err := querier.QueryLatestValset(network.Context)
	assert.NoError(t, err)
	require.NotNil(t, latestValset)
	assert.Equal(t, 4, len(latestValset.Members))

	// check whether the QGB contract was deployed
	bridge, err := network.GetLatestDeployedQGBContract(network.Context)
	HandleNetworkError(t, network, err, false)

	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	// check whether the relayer relayed all attestations
	eventNonce, err := evmClient.StateLastEventNonce(&bind.CallOpts{Context: network.Context})
	assert.NoError(t, err)

	// attestations are either data commitments or valsets
	latestNonce, err := querier.QueryLatestAttestationNonce(network.Context)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, eventNonce, latestNonce-1)
}
