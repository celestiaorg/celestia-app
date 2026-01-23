package types

import authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

// ModuleName is the name of the feeaddress module.
const ModuleName = "feeaddress"

// ProtocolFeeGasLimit is the gas limit for the protocol-injected fee forward
// transaction. Set to 40000 which provides sufficient gas for:
// - Message decoding and routing (~1000 gas)
// - Bank transfer via SendCoinsFromAccountToModule (~20000 gas)
// - Safety margin for SDK overhead and future changes
// This value is validated in ProcessProposal to prevent malicious manipulation.
const ProtocolFeeGasLimit = 40000

// FeeAddress is the module account address for the feeaddress module.
var FeeAddress = authtypes.NewModuleAddress(ModuleName)

// FeeAddressBech32 is the bech32-encoded fee address.
var FeeAddressBech32 string

func init() {
	FeeAddressBech32 = FeeAddress.String()
}
