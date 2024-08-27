package app_test

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	icagenesistypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/genesis/types"
	icahostkeeper "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/keeper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIcaModule(t *testing.T) {
	t.Run("DefaultGenesis should return custom genesis state", func(t *testing.T) {
		icaModule := app.NewIcaModule(icahostkeeper.Keeper{})
		cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		genesis := icaModule.DefaultGenesis(cdc.Codec)

		got := icagenesistypes.GenesisState{}
		err := json.Unmarshal(genesis, &got)
		require.NoError(t, err)

		want := "[\"/ibc.applications.transfer.v1.MsgTransfer\",\"/cosmos.bank.v1beta1.MsgSend\",\"/cosmos.staking.v1beta1.MsgDelegate\",\"/cosmos.staking.v1beta1.MsgBeginRedelegate\",\"/cosmos.staking.v1beta1.MsgUndelegate\",\"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation\",\"/cosmos.distribution.v1beta1.MsgSetWithdrawAddress\",\"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward\",\"/cosmos.distribution.v1beta1.MsgFundCommunityPool\",\"/cosmos.gov.v1.MsgVote\",\"/cosmos.feegrant.v1beta1.MsgGrantAllowance\",\"/cosmos.feegrant.v1beta1.MsgRevokeAllowance\"]"
		assert.Equal(t, want, got.HostGenesisState.Params.AllowMessages)
		assert.True(t, got.HostGenesisState.Params.HostEnabled)
		assert.False(t, got.ControllerGenesisState.Params.ControllerEnabled)
	})
}
