package types

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	// ModuleName is the name of the burn module.
	ModuleName = "burn"

	// StoreKey is the store key for the burn module.
	StoreKey = ModuleName

	// AddressLength is the standard Cosmos SDK address length in bytes.
	AddressLength = 20
)

// TotalBurnedKey is the key for storing cumulative burned tokens.
var TotalBurnedKey = []byte("TotalBurned")

// BurnAddress is the address where tokens are sent to be burned.
// This is a vanity address derived from 20 zero bytes (32 'q' characters in bech32).
var BurnAddress = sdk.AccAddress(make([]byte, AddressLength))

// BurnAddressBech32 is the bech32-encoded burn address.
const BurnAddressBech32 = "celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqzf30as"

func init() {
	// Verify BurnAddressBech32 matches the derived BurnAddress.
	// This catches any inconsistency between the two definitions.
	derived := sdk.MustBech32ifyAddressBytes("celestia", BurnAddress)
	if derived != BurnAddressBech32 {
		panic("BurnAddressBech32 does not match derived BurnAddress: got " + derived + ", want " + BurnAddressBech32)
	}
}
