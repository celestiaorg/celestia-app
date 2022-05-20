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

	network, err := NewQGBNetwork()
	HandleNetworkError(t, network, err, false)

	// preferably, run this also when ctrl+c
	defer network.DeleteAll() //nolint:errcheck

	err = network.StartMinimal()
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(ctx, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	bridge, err := network.GetLatestDeployedQGBContract(ctx)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForRelayerToStart(ctx, bridge)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), vsNonce)

	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), dcNonce)
}

func TestRelayerWithTwoValidators(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork()
	HandleNetworkError(t, network, err, false)

	// preferably, run this also when ctrl+c
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
	err = network.WaitForBlock(ctx, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	bridge, err := network.GetLatestDeployedQGBContract(ctx)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForRelayerToStart(ctx, bridge)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), dcNonce)

	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(2), vsNonce)
}

func TestRelayerWithMultipleValidators(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork()
	HandleNetworkError(t, network, err, false)

	// preferably, run this also when ctrl+c
	defer network.DeleteAll() //nolint:errcheck

	// start full network with four validatorS
	err = network.StartAll()
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(ctx, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE2ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(ctx, CORE3ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// check whether the four validators are up and running
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err, false)

	lastValsets, err := querier.QueryLastValsets(ctx)
	assert.NoError(t, err)
	assert.Equal(t, 4, len(lastValsets[0].Members))

	bridge, err := network.GetLatestDeployedQGBContract(ctx)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForRelayerToStart(ctx, bridge)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), dcNonce)

	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, uint64(2), vsNonce)
}
