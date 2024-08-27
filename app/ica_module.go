package app

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/module"
	ica "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts"
	icagenesistypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/genesis/types"
	icahostkeeper "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/keeper"
)

const (
	arabicaChainID = "arabica-11"
	mochaChainID   = "mocha-4"
)

var (
	// IcaModule implements the AppModule interface.
	_ module.AppModule = IcaModule{}
)

// IcaModule is a wrapper around the ICA module that allows for a custom DefaultGenesis function.
type IcaModule struct {
	ica.AppModule
}

// NewIcaModule creates a new ICA module with a custom DefaultGenesis function.
func NewIcaModule(chainID string, icaHostKeeper icahostkeeper.Keeper) module.AppModule {
	if chainID == arabicaChainID || chainID == mochaChainID {
		// These testnets went through the v2 activation height on a
		// celestia-app release that did not use a custom DefaultGenesis
		// function.
		return ica.NewAppModule(nil, &icaHostKeeper)
	}

	// This IcaModule has a custom DefaultGenesis function.
	return IcaModule{
		ica.NewAppModule(nil, &icaHostKeeper),
	}
}

// DefaultGenesis returns custom ICA module genesis state.
func (IcaModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return icaCustomGenesis(cdc)
}

func icaCustomGenesis(cdc codec.JSONCodec) json.RawMessage {
	gs := icagenesistypes.DefaultGenesis()
	gs.HostGenesisState.Params.AllowMessages = icaAllowMessages()
	gs.HostGenesisState.Params.HostEnabled = true
	return cdc.MustMarshalJSON(gs)
}
