package app_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaimRewardsAfterFullUndelegation tests the scenario where:
// 1. A user delegates to a validator
// 2. Earns some rewards
// 3. Undelegates entirely
// 4. Claims rewards
//
// Inspired by https://github.com/celestiaorg/celestia-app/issues/5381
func TestClaimRewardsAfterFullUndelegation(t *testing.T) {
	accounts := testnode.RandomAccounts(2)
	config := testnode.DefaultConfig().WithFundedAccounts(accounts...)
	cctx, _, _ := testnode.NewNetwork(t, config)
	txClient, err := testnode.NewTxClientFromContext(cctx)
	require.NoError(t, err)

	delegatorName := accounts[0]
	keyring := cctx.Keyring

	record, err := keyring.Key(delegatorName)
	require.NoError(t, err)

	delegatorAccAddress, err := record.GetAddress()
	require.NoError(t, err)

	delegationAmount := math.NewInt(1_000_000_000) // 1000 TIA

	stakingClient := stakingtypes.NewQueryClient(cctx.GRPCClient)
	validatorsResp, err := stakingClient.Validators(cctx.GoContext(), &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	require.Greater(t, len(validatorsResp.Validators), 0)
	validatorAddress := validatorsResp.Validators[0].OperatorAddress
	delegatorAddress := delegatorAccAddress.String()

	// Step 1: Delegate to validator
	delegateMsg := &stakingtypes.MsgDelegate{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
		Amount:           types.NewCoin(appconsts.BondDenom, delegationAmount),
	}

	delegateRes, err := txClient.SubmitTx(cctx.GoContext(), []types.Msg{delegateMsg}, user.SetGasLimit(200_000))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, delegateRes.Code)

	// Wait for transaction to be included
	err = cctx.WaitForNextBlock()
	require.NoError(t, err)

	// Verify delegation exists
	delegationResp, err := stakingClient.Delegation(cctx.GoContext(), &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: delegatorAddress,
		ValidatorAddr: validatorAddress,
	})
	require.NoError(t, err)
	assert.Equal(t, delegationAmount.String(), delegationResp.DelegationResponse.Balance.Amount.String())

	// Step 2: Wait for rewards to accumulate (advance several blocks)
	for i := 0; i < 3; i++ {
		err = cctx.WaitForNextBlock()
		require.NoError(t, err)
	}

	// Verify rewards exist
	distributionClient := distributiontypes.NewQueryClient(cctx.GRPCClient)
	rewardsResp, err := distributionClient.DelegationRewards(cctx.GoContext(), &distributiontypes.QueryDelegationRewardsRequest{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	})
	require.NoError(t, err)
	require.Greater(t, len(rewardsResp.Rewards), 0)
	t.Logf("Rewards before undelegation: %v", rewardsResp.Rewards)

	// Step 3: Undelegate entirely
	undelegateMsg := &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
		Amount:           types.NewCoin(appconsts.BondDenom, delegationAmount),
	}

	undelegateRes, err := txClient.SubmitTx(cctx.GoContext(), []types.Msg{undelegateMsg}, user.SetGasLimit(200000))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, undelegateRes.Code)

	// Wait for transaction to be included
	err = cctx.WaitForNextBlock()
	require.NoError(t, err)

	// Verify delegation no longer exists
	_, err = stakingClient.Delegation(cctx.GoContext(), &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: delegatorAddress,
		ValidatorAddr: validatorAddress,
	})
	assert.Error(t, err, "delegation should not exist after full undelegation")

	// Check if the rewards can be accessed, currently we expect not
	_, err = distributionClient.DelegationRewards(cctx.GoContext(), &distributiontypes.QueryDelegationRewardsRequest{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no delegation for (address, validator) tupl")

	// Step 4: Try to claim rewards and expect no error
	withdrawRewardsMsg := &distributiontypes.MsgWithdrawDelegatorReward{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	}

	withdrawRes, err := txClient.SubmitTx(cctx.GoContext(), []types.Msg{withdrawRewardsMsg}, user.SetGasLimit(200000))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, withdrawRes.Code)
	fmt.Printf("Withdraw rewards response: %v\n", withdrawRes)
}
