package app

import (
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/tendermint/spm/cosmoscmd"
)

// RegisterAccountInterface registers the authtypes.AccountI interface to the
// interface registery in the provided encoding config
// todo(evan): check if this is still needed
func RegisterAccountInterface() cosmoscmd.EncodingConfig {
	conf := cosmoscmd.MakeEncodingConfig(ModuleBasics)
	conf.InterfaceRegistry.RegisterInterface(
		"cosmos.auth.v1beta1.BaseAccount",
		(*authtypes.AccountI)(nil),
	)
	conf.InterfaceRegistry.RegisterImplementations(
		(*authtypes.AccountI)(nil),
		&authtypes.BaseAccount{},
	)
	return conf
}
