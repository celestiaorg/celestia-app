package app_test

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	icagenesistypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/genesis/types"
	icahostkeeper "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIcaModule(t *testing.T) {
	type testCase struct {
		name              string
		chainID           string
		wantAllowMessages []string
	}

	testCases := []testCase{
		{
			name:              "arabica should return default allow messages",
			chainID:           "arabica-11",
			wantAllowMessages: []string{"*"},
		},
		{
			name:              "mocha should return default allow messages",
			chainID:           "mocha-4",
			wantAllowMessages: []string{"*"},
		},
		{
			name:              "mainnet should return custom allow messages",
			chainID:           "celestia",
			wantAllowMessages: []string{"/ibc.applications.transfer.v1.MsgTransfer", "/cosmos.bank.v1beta1.MsgSend", "/cosmos.staking.v1beta1.MsgDelegate", "/cosmos.staking.v1beta1.MsgBeginRedelegate", "/cosmos.staking.v1beta1.MsgUndelegate", "/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation", "/cosmos.distribution.v1beta1.MsgSetWithdrawAddress", "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward", "/cosmos.distribution.v1beta1.MsgFundCommunityPool", "/cosmos.gov.v1.MsgVote", "/cosmos.feegrant.v1beta1.MsgGrantAllowance", "/cosmos.feegrant.v1beta1.MsgRevokeAllowance"},
		},
		{
			name:              "random should return custom allow messages",
			chainID:           "random",
			wantAllowMessages: []string{"/ibc.applications.transfer.v1.MsgTransfer", "/cosmos.bank.v1beta1.MsgSend", "/cosmos.staking.v1beta1.MsgDelegate", "/cosmos.staking.v1beta1.MsgBeginRedelegate", "/cosmos.staking.v1beta1.MsgUndelegate", "/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation", "/cosmos.distribution.v1beta1.MsgSetWithdrawAddress", "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward", "/cosmos.distribution.v1beta1.MsgFundCommunityPool", "/cosmos.gov.v1.MsgVote", "/cosmos.feegrant.v1beta1.MsgGrantAllowance", "/cosmos.feegrant.v1beta1.MsgRevokeAllowance"},
		},
	}

	for _, tc := range testCases {
		codec := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec

		t.Run(tc.name, func(t *testing.T) {
			icaModule := app.NewIcaModule(tc.chainID, icahostkeeper.Keeper{})
			genesis := icaModule.DefaultGenesis(codec)

			got := icagenesistypes.GenesisState{}
			err := json.Unmarshal(genesis, &got)
			require.NoError(t, err)

			assert.True(t, got.HostGenesisState.Params.HostEnabled)
			assert.Equal(t, tc.wantAllowMessages, got.HostGenesisState.Params.AllowMessages)
		})
	}
}
