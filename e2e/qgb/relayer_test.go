package e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRelayerWithOneValidator(t *testing.T) {
	network, err := NewQGBNetwork()
	assert.NoError(t, err)
	err = network.StartMinimal()
	assert.NoError(t, err)
	// preferably, run this also when ctrl+c
	defer network.DeleteAll() //nolint:errcheck
	ctx := context.TODO()
	err = network.WaitForBlock(ctx, 15)
	assert.NoError(t, err)

	bridge, err := GetLatestDeployedQGBContract(ctx, network.EVMRPC)
	assert.NoError(t, err)

	// FIXME should we use the evm client here or go for raw queries?
	evmClient := orchestrator.NewEvmClient(nil, *bridge, nil, network.EVMRPC)

	vsNonce, err := evmClient.StateLastValsetNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Equal(t, vsNonce, uint64(1))

	dcNonce, err := evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{Context: ctx})
	assert.NoError(t, err)
	assert.Greater(t, dcNonce, uint64(1))
}
