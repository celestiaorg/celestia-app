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

func TestRelayerWithOneValidator(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	HandleNetworkError(t, network, err, false)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	err = network.StartMinimal()
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	bridge, err := network.GetLatestDeployedQGBContract(network.Context)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForRelayerToStart(network.Context, bridge)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, bridge, nil, network.EVMRPC)

	vsNonce, err := evmClient.StateLastEventNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, vsNonce, uint64(2))
}

func TestRelayerWithTwoValidators(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	HandleNetworkError(t, network, err, false)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	// start minimal network with one validator
	err = network.StartMinimal()
	HandleNetworkError(t, network, err, false)

	// add second validator
	err = network.Start(Core1)
	HandleNetworkError(t, network, err, false)

	// add second orchestrator
	err = network.Start(Core1Orch)
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	bridge, err := network.GetLatestDeployedQGBContract(network.Context)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForRelayerToStart(network.Context, bridge)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, bridge, nil, network.EVMRPC)

	dcNonce, err := evmClient.StateLastEventNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, dcNonce, uint64(3))
}

func TestRelayerWithMultipleValidators(t *testing.T) {
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

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE2ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE3ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// check whether the four validators are up and running
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil, network.EncCfg)
	HandleNetworkError(t, network, err, false)

	latestValset, err := querier.QueryLatestValset(network.Context)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(latestValset.Members))

	bridge, err := network.GetLatestDeployedQGBContract(network.Context)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForRelayerToStart(network.Context, bridge)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, bridge, nil, network.EVMRPC)

	dcNonce, err := evmClient.StateLastEventNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, dcNonce, uint64(3))
}
