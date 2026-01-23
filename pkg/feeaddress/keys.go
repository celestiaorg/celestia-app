// Package feeaddress provides functionality for forwarding TIA tokens to the fee collector.
// Tokens sent to the fee address are automatically forwarded to validators via protocol-injected
// transactions in PrepareProposal.
//
// This package contains types and utilities; the actual forwarding logic is implemented
// in app/ante/protocol_fee.go (ProtocolFeeTerminatorDecorator).
package feeaddress

import authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

// ModuleName is the name used for the module account.
// Although there is no x/feeaddress module, we keep this name for:
// - Module account registration in maccPerms
// - Address derivation via NewModuleAddress
const ModuleName = "feeaddress"

// ProtocolFeeGasLimit is the gas limit for the protocol-injected fee forward
// transaction. Set to 40000 which provides sufficient gas for:
// - Message decoding and routing (~1000 gas)
// - Bank transfer via SendCoinsFromAccountToModule (~20000 gas)
// - Safety margin for SDK overhead and future changes
// This value is validated in ProcessProposal to prevent malicious manipulation.
const ProtocolFeeGasLimit = 40000

// FeeAddress is the module account address for the feeaddress module.
// Derived from ModuleName using standard Cosmos SDK module account derivation.
// Address: celestia18sjk23yldd9dg7j33sk24elwz2f06zt7ahx39y
var FeeAddress = authtypes.NewModuleAddress(ModuleName)

// FeeAddressBech32 is the bech32-encoded fee address.
var FeeAddressBech32 string

func init() {
	FeeAddressBech32 = FeeAddress.String()
}
