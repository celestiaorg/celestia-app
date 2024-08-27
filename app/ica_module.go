package app

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	ica "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts"
	icagenesistypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/genesis/types"
	icahostkeeper "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/keeper"
)

// IcaModule is a wrapper around the ICA module that allows for a custom DefaultGenesis function.
type IcaModule struct {
	ica.AppModule
}

// NewICAModule creates a new ICA module with a custom DefaultGenesis function.
func NewICAModule(icaHostKeeper icahostkeeper.Keeper) IcaModule {
	return IcaModule{
		ica.NewAppModule(nil, &icaHostKeeper),
	}
}

// DefaultGenesis returns custom ICA module genesis state.
func (IcaModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return customGenesis(cdc)
}

func customGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := icagenesistypes.DefaultGenesis()
	gs.HostGenesisState.Params.AllowMessages = IcaAllowMessages()
	gs.HostGenesisState.Params.HostEnabled = true
	gs.ControllerGenesisState.Params.ControllerEnabled = false
	return cdc.MustMarshalJSON(gs)
}
