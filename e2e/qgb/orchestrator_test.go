package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	err = network.WaitForBlock(network.Context, int64(network.DataCommitmentWindow+50))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// give the orchestrators some time to catchup
	time.Sleep(30 * time.Second)

	// FIXME should we use the querier here or go for raw queries?
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil, network.EncCfg)
	HandleNetworkError(t, network, err, false)

	vsConfirm, err := querier.QueryValsetConfirm(ctx, 1, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	require.NotNil(t, vsConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, vsConfirm.EthAddress)

	dcConfirm, err := querier.QueryDataCommitmentConfirm(ctx, network.DataCommitmentWindow, 0, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	require.NoError(t, err)
	require.NotNil(t, dcConfirm)
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
	err = network.WaitForBlock(network.Context, int64(network.DataCommitmentWindow+50))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// give the orchestrators some time to catchup
	time.Sleep(30 * time.Second)

	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil, network.EncCfg)
	HandleNetworkError(t, network, err, false)

	// check core0 submited the valset confirm
	core0ValsetConfirm, err := querier.QueryValsetConfirm(ctx, 1, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	assert.NoError(t, err)
	assert.NotNil(t, core0ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0ValsetConfirm.EthAddress)

	// check core0 submitted the data commitment confirm
	core0DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		network.DataCommitmentWindow,
		0,
		CORE0ACCOUNTADDRESS,
	)
	// assert the confirm exist
	require.NoError(t, err)
	require.NotNil(t, core0DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0DataCommitmentConfirm.EthAddress)

	// get the last valset where all validators were created
	vs, err := network.GetValsetContainingVals(ctx, 2)
	require.NoError(t, err)
	require.NotNil(t, vs)

	// check core1 submited the attestation confirm
	core1Confirm, err := network.GetAttestationConfirm(ctx, vs.Nonce+1, CORE1ACCOUNTADDRESS)
	require.NoError(t, err)
	require.NotNil(t, core1Confirm)
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
	err = network.WaitForBlock(network.Context, int64(network.DataCommitmentWindow+50))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE2ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE3ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// give the orchestrators some time to catchup
	time.Sleep(30 * time.Second)

	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil, network.EncCfg)
	HandleNetworkError(t, network, err, false)

	// check core0 submited the valset confirm
	core0ValsetConfirm, err := querier.QueryValsetConfirm(ctx, 1, CORE0ACCOUNTADDRESS)
	// check the confirm exist
	require.NoError(t, err)
	require.NotNil(t, core0ValsetConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0ValsetConfirm.EthAddress)

	// check core0 submitted the data commitment confirm
	core0DataCommitmentConfirm, err := querier.QueryDataCommitmentConfirm(
		ctx,
		network.DataCommitmentWindow,
		0,
		CORE0ACCOUNTADDRESS,
	)
	// check the confirm exist
	require.NoError(t, err)
	require.NotNil(t, core0DataCommitmentConfirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, core0DataCommitmentConfirm.EthAddress)

	// get the last valset where all validators were created
	vs, err := network.GetValsetContainingVals(ctx, 4)
	require.NoError(t, err)
	require.NotNil(t, vs)

	// check core1 submited the attestation confirm
	core1Confirm, err := network.GetAttestationConfirm(ctx, vs.Nonce+1, CORE1ACCOUNTADDRESS)
	require.NoError(t, err)
	require.NotNil(t, core1Confirm)

	// check core2 submited the attestation confirm
	core2Confirm, err := network.GetAttestationConfirm(ctx, vs.Nonce+1, CORE2ACCOUNTADDRESS)
	require.NoError(t, err)
	require.NotNil(t, core2Confirm)

	// check core3 submited the attestation confirm
	core3Confirm, err := network.GetAttestationConfirm(ctx, vs.Nonce+1, CORE3ACCOUNTADDRESS)
	require.NoError(t, err)
	require.NotNil(t, core3Confirm)
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
	err = network.WaitForBlock(network.Context, int64(2*network.DataCommitmentWindow))
	HandleNetworkError(t, network, err, false)

	// add core0  orchestrator
	err = network.Start(Core0Orch)
	HandleNetworkError(t, network, err, false)

	// add core1 orchestrator
	err = network.Start(Core1Orch)
	HandleNetworkError(t, network, err, false)

	// give time for the orchestrators to submit confirms
	err = network.WaitForBlock(network.Context, int64(2*network.DataCommitmentWindow+50))
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE0ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	err = network.WaitForOrchestratorToStart(network.Context, CORE1ACCOUNTADDRESS)
	HandleNetworkError(t, network, err, false)

	// give the orchestrators some time to catchup
	time.Sleep(30 * time.Second)

	// FIXME should we use the querier here or go for raw queries?
	querier, err := orchestrator.NewQuerier(network.CelestiaGRPC, network.TendermintRPC, nil, network.EncCfg)
	HandleNetworkError(t, network, err, false)

	// check core0 submitted valset 1 confirm
	vs1Core0Confirm, err := querier.QueryValsetConfirm(ctx, 1, CORE0ACCOUNTADDRESS)
	// assert the confirm exist
	require.NoError(t, err)
	require.NotNil(t, vs1Core0Confirm)
	// assert that it carries the right eth address
	assert.Equal(t, CORE0EVMADDRESS, vs1Core0Confirm.EthAddress)

	// get the last valset where all validators were created
	vs, err := network.GetValsetContainingVals(ctx, 2)
	require.NoError(t, err)
	require.NotNil(t, vs)

	latestNonce, err := querier.QueryLatestAttestationNonce(ctx)
	require.NoError(t, err)

	// checks that all nonces where all validators were part of the valset were signed
	for i := vs.Nonce + 1; i <= latestNonce; i++ {
		// check core1 submited the attestation confirm
		core1Confirm, err := network.GetAttestationConfirm(ctx, i, CORE1ACCOUNTADDRESS)
		require.NoError(t, err)
		require.NotNil(t, core1Confirm)
	}
}
