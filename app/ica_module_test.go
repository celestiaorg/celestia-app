package app_test

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	icagenesistypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/genesis/types"
	icahostkeeper "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/keeper"
	"github.com/stretchr/testify/assert"
)

func TestIcaModule(t *testing.T) {
	t.Run("DefaultGenesis should return custom genesis state", func(t *testing.T) {
		icaModule := app.NewICAModule(icahostkeeper.Keeper{})
		cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		genesis := icaModule.DefaultGenesis(cdc.Codec)

		got := icagenesistypes.GenesisState{}
		json.Unmarshal(genesis, &got)

		assert.Equal(t, app.IcaAllowMessages(), got.HostGenesisState.Params.AllowMessages)
		assert.True(t, got.HostGenesisState.Params.HostEnabled)
		assert.False(t, got.ControllerGenesisState.Params.ControllerEnabled)
	})
}
