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
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
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

	stakingClient := stakingtypes.NewQueryClient(cctx.GRPCClient)
	distributionClient := distributiontypes.NewQueryClient(cctx.GRPCClient)

	delegatorAddress := getDelegatorAddress(t, &cctx, accounts)
	validatorAddress := getValidatorAddress(t, &cctx)

	delegationAmount := math.NewInt(1_000_000_000_000) // 1,000,000 TIA
	amount := types.NewCoin(appconsts.BondDenom, delegationAmount)

	delegateToValidator(t, &cctx, txClient, delegatorAddress, validatorAddress, amount)
	verifyDelegationExists(t, &cctx, stakingClient, delegatorAddress, validatorAddress, delegationAmount)
	verifyRewardsExist(t, &cctx, distributionClient, delegatorAddress, validatorAddress)

	undelegate(t, &cctx, txClient, delegatorAddress, validatorAddress, amount)
	verifyDelegationDoesNotExist(t, &cctx, stakingClient, delegatorAddress, validatorAddress)
	verifyDelegationRewardsDoNotExist(t, &cctx, distributionClient, delegatorAddress, validatorAddress)

	claimRewards(t, &cctx, txClient, delegatorAddress, validatorAddress, amount)
}

func getDelegatorAddress(t *testing.T, cctx *testnode.Context, accounts []string) string {
	keyring := cctx.Keyring
	delegatorName := accounts[0]

	record, err := keyring.Key(delegatorName)
	require.NoError(t, err)

	delegatorAccAddress, err := record.GetAddress()
	require.NoError(t, err)
	return delegatorAccAddress.String()
}

func getValidatorAddress(t *testing.T, cctx *testnode.Context) string {
	stakingClient := stakingtypes.NewQueryClient(cctx.GRPCClient)

	validatorsResp, err := stakingClient.Validators(cctx.GoContext(), &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	require.Greater(t, len(validatorsResp.Validators), 0)

	return validatorsResp.Validators[0].OperatorAddress
}

func delegateToValidator(t *testing.T, cctx *testnode.Context, txClient *user.TxClient, delegatorAddress, validatorAddress string, amount types.Coin) {
	delegateMsg := &stakingtypes.MsgDelegate{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
		Amount:           amount,
	}

	delegateRes, err := txClient.SubmitTx(cctx.GoContext(), []types.Msg{delegateMsg}, user.SetGasLimit(200_000))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, delegateRes.Code)
}

func verifyDelegationExists(t *testing.T, cctx *testnode.Context, stakingClient stakingtypes.QueryClient, delegatorAddress, validatorAddress string, delegationAmount math.Int) {
	delegationResp, err := stakingClient.Delegation(cctx.GoContext(), &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: delegatorAddress,
		ValidatorAddr: validatorAddress,
	})
	require.NoError(t, err)
	assert.Equal(t, delegationAmount.String(), delegationResp.DelegationResponse.Balance.Amount.String())
}

func verifyRewardsExist(t *testing.T, cctx *testnode.Context, distributionClient distributiontypes.QueryClient, delegatorAddress, validatorAddress string) {
	rewardsResp, err := distributionClient.DelegationRewards(cctx.GoContext(), &distributiontypes.QueryDelegationRewardsRequest{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	})
	require.NoError(t, err)
	require.Greater(t, len(rewardsResp.Rewards), 0)
	t.Logf("Rewards before undelegation: %v", rewardsResp.Rewards)
}

func undelegate(t *testing.T, cctx *testnode.Context, txClient *user.TxClient, delegatorAddress, validatorAddress string, amount types.Coin) {
	undelegateMsg := &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
		Amount:           amount,
	}

	undelegateRes, err := txClient.SubmitTx(cctx.GoContext(), []types.Msg{undelegateMsg}, user.SetGasLimit(200_000))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, undelegateRes.Code)
}

func verifyDelegationDoesNotExist(t *testing.T, cctx *testnode.Context, stakingClient stakingtypes.QueryClient, delegatorAddress, validatorAddress string) {
	_, err := stakingClient.Delegation(cctx.GoContext(), &stakingtypes.QueryDelegationRequest{
		DelegatorAddr: delegatorAddress,
		ValidatorAddr: validatorAddress,
	})
	assert.Error(t, err)
	assert.ErrorContains(t, err, fmt.Sprintf("delegation with delegator %s not found for validator %s", delegatorAddress, validatorAddress))
}

func verifyDelegationRewardsDoNotExist(t *testing.T, cctx *testnode.Context, distributionClient distributiontypes.QueryClient, delegatorAddress, validatorAddress string) {
	_, err := distributionClient.DelegationRewards(cctx.GoContext(), &distributiontypes.QueryDelegationRewardsRequest{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no delegation for (address, validator) tupl")
}

func claimRewards(t *testing.T, cctx *testnode.Context, txClient *user.TxClient, delegatorAddress string, validatorAddress string, amount types.Coin) {
	withdrawRewardsMsg := &distributiontypes.MsgWithdrawDelegatorReward{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	}
	withdrawRes, err := txClient.SubmitTx(cctx.GoContext(), []types.Msg{withdrawRewardsMsg}, user.SetGasLimit(200000))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, withdrawRes.Code)
	txHash := withdrawRes.TxHash

	txServiceClient := txtypes.NewServiceClient(cctx.GRPCClient)
	getTxResp, err := txServiceClient.GetTx(cctx.GoContext(), &txtypes.GetTxRequest{Hash: txHash})
	require.NoError(t, err)
	require.NotNil(t, getTxResp.TxResponse)
	require.Equal(t, abci.CodeTypeOK, getTxResp.TxResponse.Code)

	event, err := getWithdrawRewardsEvent(t, getTxResp.TxResponse.Events)
	require.NoError(t, err)
	require.Equal(t, delegatorAddress, event.DelegatorAddress)
	require.Equal(t, validatorAddress, event.ValidatorAddress)
}

func getWithdrawRewardsEvent(t *testing.T, events []abci.Event) (WithdrawRewardsEvent, error) {
	for _, event := range events {
		if event.Type == distributiontypes.EventTypeWithdrawRewards {
			var delegatorAddress string
			var validatorAddress string
			var amount string
			for _, attr := range event.Attributes {
				switch attr.Key {
				case distributiontypes.AttributeKeyDelegator:
					delegatorAddress = attr.Value
				case distributiontypes.AttributeKeyValidator:
					validatorAddress = attr.Value
				case "amount":
					amount = attr.Value
				}
			}
			return WithdrawRewardsEvent{
				DelegatorAddress: delegatorAddress,
				ValidatorAddress: validatorAddress,
				Amount:           amount,
			}, nil
		}
	}
	return WithdrawRewardsEvent{}, fmt.Errorf("withdraw_rewards event not found")
}

type WithdrawRewardsEvent struct {
	DelegatorAddress string
	ValidatorAddress string
	Amount           string
}
