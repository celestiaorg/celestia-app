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
// This is a vanity address that encodes to "nullnullnullnullnullnullnullnull" in bech32,
// making it recognizable as a null/void destination.
var BurnAddress = sdk.AccAddress([]byte{
	0x9F, 0x3F, 0xF9, 0xF3, 0xFF,
	0x9F, 0x3F, 0xF9, 0xF3, 0xFF,
	0x9F, 0x3F, 0xF9, 0xF3, 0xFF,
	0x9F, 0x3F, 0xF9, 0xF3, 0xFF,
})

// BurnAddressBech32 is the bech32-encoded burn address.
const BurnAddressBech32 = "celestia1nullnullnullnullnullnullnullnull8qanmn"

func init() {
	// Verify BurnAddressBech32 matches the derived BurnAddress.
	// This catches any inconsistency between the two definitions.
	derived := sdk.MustBech32ifyAddressBytes("celestia", BurnAddress)
	if derived != BurnAddressBech32 {
		panic("BurnAddressBech32 does not match derived BurnAddress: got " + derived + ", want " + BurnAddressBech32)
	}
}
