package params

import (
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// Bech32PrefixAccAddr defines the bech32 prefix of an account's address.
	Bech32PrefixAccAddr = "celestia"

	// BondDenom defines the native staking token denomination.
	BondDenom = appconsts.BondDenom
	// BondDenomAlias defines an alias for BondDenom.
	BondDenomAlias = "microtia"
	// DisplayDenom defines the name, symbol, and display value of the Celestia token.
	DisplayDenom = "TIA"
)

var (
	// Bech32PrefixAccPub defines the Bech32 prefix of an account's public key.
	Bech32PrefixAccPub = Bech32PrefixAccAddr + sdk.PrefixPublic
	// Bech32PrefixValAddr defines the Bech32 prefix of a validator's operator address.
	Bech32PrefixValAddr = Bech32PrefixAccAddr + sdk.PrefixValidator + sdk.PrefixOperator
	// Bech32PrefixValPub defines the Bech32 prefix of a validator's operator public key.
	Bech32PrefixValPub = Bech32PrefixAccAddr + sdk.PrefixValidator + sdk.PrefixOperator + sdk.PrefixPublic
	// Bech32PrefixConsAddr defines the Bech32 prefix of a consensus node address.
	Bech32PrefixConsAddr = Bech32PrefixAccAddr + sdk.PrefixValidator + sdk.PrefixConsensus
	// Bech32PrefixConsPub defines the Bech32 prefix of a consensus node public key.
	Bech32PrefixConsPub = Bech32PrefixAccAddr + sdk.PrefixValidator + sdk.PrefixConsensus + sdk.PrefixPublic
)

func init() {
	setAddressPrefixes()
}

func setAddressPrefixes() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(Bech32PrefixAccAddr, Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(Bech32PrefixValAddr, Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(Bech32PrefixConsAddr, Bech32PrefixConsPub)
	config.Seal()
}
