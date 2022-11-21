package qgb_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/staking/teststaking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/qgb"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttestationCreationWhenStartingTheChain(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set if not available
	qgb.EndBlocker(ctx, *pk)
	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation, found, err := pk.GetAttestationByNonce(ctx, 1)
	require.True(t, found)
	require.Nil(t, err)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())
}

func TestValsetCreationWhenValsetChanges(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// run abci methods after chain init
	staking.EndBlocker(input.Context, input.StakingKeeper)
	qgb.EndBlocker(ctx, *pk)

	// current attestation nonce should be 1 because a valset has been emitted upon chain init.
	currentAttestationNonce := pk.GetLatestAttestationNonce(ctx)
	require.Equal(t, uint64(1), currentAttestationNonce)

	input.Context = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	msgServer := stakingkeeper.NewMsgServerImpl(input.StakingKeeper)

	tests := map[string]struct {
		f             func()
		expectedNonce uint64
	}{
		"unbond validator": {
			f: func() {
				undelegateMsg := testutil.NewTestMsgUnDelegateValidator(testutil.ValAddrs[0], testutil.StakingAmount)
				_, err := msgServer.Undelegate(input.Context, undelegateMsg)
				require.NoError(t, err)
				staking.EndBlocker(input.Context, input.StakingKeeper)
				qgb.EndBlocker(input.Context, *pk)
			},
			expectedNonce: currentAttestationNonce + 1,
		},
		"edit validator: new orch address": {
			f: func() {
				newOrchAddr := sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
				editMsg := stakingtypes.NewMsgEditValidator(
					testutil.ValAddrs[1],
					stakingtypes.Description{},
					nil,
					nil,
					&newOrchAddr,
					nil,
				)
				_, err := msgServer.EditValidator(input.Context, editMsg)
				require.NoError(t, err)
				staking.EndBlocker(input.Context, input.StakingKeeper)
				qgb.EndBlocker(input.Context, *pk)
			},
			expectedNonce: currentAttestationNonce + 2,
		},
		"edit validator: new evm address": {
			f: func() {
				newEVMAddr, err := teststaking.RandomEVMAddress()
				require.NoError(t, err)
				editMsg := stakingtypes.NewMsgEditValidator(
					testutil.ValAddrs[1],
					stakingtypes.Description{},
					nil,
					nil,
					nil,
					newEVMAddr,
				)
				_, err = msgServer.EditValidator(input.Context, editMsg)
				require.NoError(t, err)
				staking.EndBlocker(input.Context, input.StakingKeeper)
				qgb.EndBlocker(input.Context, *pk)
			},
			expectedNonce: currentAttestationNonce + 3,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tc.f()
			assert.Equal(t, tc.expectedNonce, pk.GetLatestAttestationNonce(ctx))
		})
	}
}

func TestFirstAttestationIsValset(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	// EndBlocker should set a new validator set
	qgb.EndBlocker(ctx, *pk)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
	attestation, found, err := pk.GetAttestationByNonce(ctx, 1)
	require.Nil(t, err)
	require.True(t, found)
	require.NotNil(t, attestation)
	require.Equal(t, uint64(1), attestation.GetNonce())

	// get the valsets
	require.Equal(t, types.ValsetRequestType, attestation.Type())
	vs, ok := attestation.(*types.Valset)
	require.True(t, ok)
	require.NotNil(t, vs)
}

func TestValsetSetting(t *testing.T) {
	input, ctx := testutil.SetupFiveValChain(t)
	pk := input.QgbKeeper

	vs, err := pk.GetCurrentValset(ctx)
	require.Nil(t, err)
	err = pk.SetAttestationRequest(ctx, &vs)
	require.Nil(t, err)

	require.Equal(t, uint64(1), pk.GetLatestAttestationNonce(ctx))
}

// Add data commitment window tests
