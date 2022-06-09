package e2e

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestOrchestratorWithOneValidator(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	HandleNetworkError(t, network, err, false)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	// start 1 validator
	err = network.StartBase()
	HandleNetworkError(t, network, err, false)

	// add orchestrator
	err = network.Start(Core0Orch)
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(types.DataCommitmentWindow+5))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the querier here or go for raw queries?
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err, false)

	vsConfirm, err := querier.QueryValsetConfirm(ctx, 1, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, vsConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, vsConfirm.EthAddress)

	dcConfirm, err := querier.QueryDataCommitmentConfirm(ctx, types.DataCommitmentWindow, 0, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, dcConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, dcConfirm.EthAddress)
}

func TestOrchestratorWithTwoValidators(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	HandleNetworkError(t, network, err, false)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	// start minimal network with one validator
	// start 1 validator
	err = network.StartBase()
	HandleNetworkError(t, network, err, false)

	// add core 0 orchestrator
	err = network.Start(Core0Orch)
	HandleNetworkError(t, network, err, false)

	// add core1 validator
	err = network.Start(Core1)
	HandleNetworkError(t, network, err, false)

	// add core1 orchestrator
	err = network.Start(Core1Orch)
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(types.DataCommitmentWindow+10))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err, false)

	// check core0 submited the valset confirm
	core0ValsetConfirm, err := querier.QueryValsetConfirm(ctx, 2, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core0ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0ValsetConfirm.EthAddress)

	// check core0 submitted the data commitment confirm
	core0DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE0ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core0DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0DataCommitmentConfirm.EthAddress)

	// check core1 submited the valset confirm
	core1ValsetConfirm, err := querier.QueryValsetConfirm(ctx, 2, CORE1ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core1ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, core1ValsetConfirm.EthAddress)

	// check core1 submitted the data commitment confirm
	core1DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE1ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core1DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, core1DataCommitmentConfirm.EthAddress)
}

func TestOrchestratorWithMultipleValidators(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	assert.NoError(t, err)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	// start full network with four validatorS
	err = network.StartAll()
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(types.DataCommitmentWindow+10))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE2ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE3ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err, false)

	lastValsets, err := querier.QueryLastValsets(ctx)
	assert.NoError(t, err)

	// check core0 submited the valset confirm
	core0ValsetConfirm, err := querier.QueryValsetConfirm(ctx, lastValsets[0].Nonce, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core0ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0ValsetConfirm.EthAddress)

	// check core0 submitted the data commitment confirm
	core0DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE0ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core0DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0DataCommitmentConfirm.EthAddress)

	// check core1 submited the valset confirm
	core1ValsetConfirm, err := querier.QueryValsetConfirm(ctx, lastValsets[0].Nonce, CORE1ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core1ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, core1ValsetConfirm.EthAddress)

	// check core1 submitted the data commitment confirm
	core1DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE1ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core1DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, core1DataCommitmentConfirm.EthAddress)

	// check core2 submited the valset confirm
	core2ValsetConfirm, err := querier.QueryValsetConfirm(ctx, lastValsets[0].Nonce, CORE2ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core2ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE2EVMADDRESS, core2ValsetConfirm.EthAddress)

	// check core1 submitted the data commitment confirm
	core2DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE2ACCOUNTADDRESS,
	)
	// assert the confirm exist
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core2DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE2EVMADDRESS, core2DataCommitmentConfirm.EthAddress)

	// check core3 submited the valset confirm
	core3ValsetConfirm, err := querier.QueryValsetConfirm(ctx, lastValsets[0].Nonce, CORE3ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core3ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE3EVMADDRESS, core3ValsetConfirm.EthAddress)

	// check core1 submitted the data commitment confirm
	core3DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE3ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core3DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE3EVMADDRESS, core3DataCommitmentConfirm.EthAddress)
}

func TestOrchestratorReplayOld(t *testing.T) {
	if os.Getenv("QGB_INTEGRATION_TEST") != TRUE {
		t.Skip("Skipping QGB integration tests")
	}

	network, err := NewQGBNetwork(context.Background())
	HandleNetworkError(t, network, err, false)

	// to release resources after tests
	defer network.DeleteAll() //nolint:errcheck

	// start 1 validator
	err = network.StartBase()
	HandleNetworkError(t, network, err, false)

	// add core1 validator
	err = network.Start(Core1)
	HandleNetworkError(t, network, err, false)

	ctx := context.TODO()
	err = network.WaitForBlock(network.Context, int64(2*types.DataCommitmentWindow))
	HandleNetworkError(t, network, err, false)

	// add core0  orchestrator
	err = network.Start(Core0Orch)
	HandleNetworkError(t, network, err, false)

	// add core1 orchestrator
	err = network.Start(Core1Orch)
	HandleNetworkError(t, network, err, false)

	// give time for the orchestrators to submit confirms
	err = network.WaitForBlock(network.Context, int64(2*types.DataCommitmentWindow+10))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// FIXME should we use the querier here or go for raw queries?
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil)
	HandleNetworkError(t, network, err, false)

	// check core0 submitted valset 1 confirm
	vs1Core0Confirm, err := querier.QueryValsetConfirm(ctx, 1, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, vs1Core0Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, vs1Core0Confirm.EthAddress)

	// check core0 submitted valset 2 confirm
	vs2Core0Confirm, err := querier.QueryValsetConfirm(ctx, 2, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, vs2Core0Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, vs2Core0Confirm.EthAddress)

	// check core1 submitted valset 1 confirm
	vs1Core1Confirm, err := querier.QueryValsetConfirm(ctx, 1, CORE1ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, vs1Core1Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, vs1Core1Confirm.EthAddress)

	// check core1 submitted valset 2 confirm
	vs2Core1Confirm, err := querier.QueryValsetConfirm(ctx, 2, CORE1ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, vs2Core1Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, vs2Core1Confirm.EthAddress)

	// check core0 submitted data commitment confirm 0->window
	dc0Core0Confirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE0ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, dc0Core0Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, dc0Core0Confirm.EthAddress)

	// check core0 submitted data commitment confirm window->2*window
	dc1Core0Confirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		2*types.DataCommitmentWindow,
		types.DataCommitmentWindow,
		CORE0ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, dc1Core0Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, dc1Core0Confirm.EthAddress)

	// check core1 submitted data commitment confirm 0->window
	dc0Core1Confirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		types.DataCommitmentWindow,
		0,
		CORE1ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, dc0Core1Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, dc0Core1Confirm.EthAddress)

	// check core0 submitted data commitment confirm window->2*window
	dc1Core1Confirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		2*types.DataCommitmentWindow,
		types.DataCommitmentWindow,
		CORE1ACCOUNTADDRESS,
	)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, dc1Core1Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE1EVMADDRESS, dc1Core1Confirm.EthAddress)
}
