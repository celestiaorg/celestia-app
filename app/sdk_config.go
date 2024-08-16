package app

import sdk "github.com/cosmos/cosmos-sdk/types"

func init() {
	maybeSetCosmosSDKConfig()
}

func maybeSetCosmosSDKConfig() {
	config := sdk.GetConfig()
	if !config.IsSealed() {
		config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
		config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
		config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
		config.Seal()
	}
}
