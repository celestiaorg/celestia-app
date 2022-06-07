package e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
	"time"
)

func TestDeployer(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork()
	HandleNetworkError(t, network, err, false)

	// preferably, run this also when ctrl+c
	defer network.DeleteAll() //nolint:errcheck

	err = network.StartMultiple(Core0, Ganache)
	HandleNetworkError(t, network, err, false)

	ctx := context.Background()
	err = network.WaitForBlock(ctx, 2)
	HandleNetworkError(t, network, err, false)

	_, err = network.GetLatestDeployedQGBContractWithCustomTimeout(ctx, 15*time.Second)
	HandleNetworkError(t, network, err, true)

	err = network.DeployQGBContract()
	HandleNetworkError(t, network, err, false)

	bridge, err := network.GetLatestDeployedQGBContract(ctx)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), vsNonce)

	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), dcNonce)
}
