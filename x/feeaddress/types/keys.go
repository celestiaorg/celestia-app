package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// ModuleName is the name of the feeaddress module.
const ModuleName = "feeaddress"

// ProtocolFeeGasLimit is the gas limit for the protocol-injected fee forward
// transaction. Set to 50000 which provides sufficient gas for:
// - Message decoding and routing (~1000 gas)
// - Bank transfer via SendCoinsFromAccountToModule (~20000 gas)
// - Event emission via EmitTypedEvent (~5000 gas)
// - Safety margin for SDK overhead and future changes
// This value is validated in ProcessProposal to prevent malicious manipulation.
const ProtocolFeeGasLimit = 50000

// FeeAddress is the address where tokens are sent to be forwarded to the fee collector.
// This is a vanity address that encodes to "feefeefeefeefeefeefeefeefeefeefeefe" in bech32,
// making it recognizable as a fee destination.
var FeeAddress = sdk.AccAddress([]byte{
	0x4E, 0x72, 0x9C, 0xE5, 0x39,
	0xCA, 0x73, 0x94, 0xE7, 0x29,
	0xCE, 0x53, 0x9C, 0xA7, 0x39,
	0x4E, 0x72, 0x9C, 0xE5, 0x39,
})

// FeeAddressBech32 is the bech32-encoded fee address.
const FeeAddressBech32 = "celestia1feefeefeefeefeefeefeefeefeefeefe8pxlcf"

func init() {
	// Verify FeeAddressBech32 matches the derived FeeAddress.
	// This catches any inconsistency between the two definitions.
	derived := sdk.MustBech32ifyAddressBytes("celestia", FeeAddress)
	if derived != FeeAddressBech32 {
		panic("FeeAddressBech32 does not match derived FeeAddress: got " + derived + ", want " + FeeAddressBech32)
	}
}
