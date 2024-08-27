package app

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	ica "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts"
)

// IcaModuleBasic defines a wrapper around the ICA module to provide a custom
// default genesis.
type IcaModuleBasic struct {
	ica.AppModuleBasic
}

// DefaultGenesis returns custom ICA module genesis state.
func (IcaModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return customGenesis(cdc)
}
