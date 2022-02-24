package qgb

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// bech32PrefixAccAddr defines the bech32 prefix of an account's address
	bech32PrefixAccAddr = "qgb"
	// bech32PrefixAccPub defines the bech32 prefix of an account's public key
	bech32PrefixAccPub = "qgbpub"
	// bech32PrefixValAddr defines the bech32 prefix of a validator's operator address
	bech32PrefixValAddr = "qgbvaloper"
	// bech32PrefixValPub defines the bech32 prefix of a validator's operator public key
	bech32PrefixValPub = "qgbvaloperpub"
	// bech32PrefixConsAddr defines the bech32 prefix of a consensus node address
	bech32PrefixConsAddr = "qgbvalcons"
	// bech32PrefixConsPub defines the bech32 prefix of a consensus node public key
	bech32PrefixConsPub = "qgbvalconspub"
)

func init() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(bech32PrefixAccAddr, bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(bech32PrefixValAddr, bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(bech32PrefixConsAddr, bech32PrefixConsPub)
	config.Seal()
}
