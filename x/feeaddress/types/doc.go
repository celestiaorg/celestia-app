// Package types contains type definitions, message types, and constants
// for the feeaddress module.
//
// Key types:
//   - MsgForwardFees: Protocol-injected message for fee forwarding
//   - EventFeeForwarded: Event emitted when fees are forwarded
//   - GenesisState: Empty genesis state for SDK compatibility
//
// Key constants:
//   - FeeAddress: The deterministic address where tokens are sent
//   - FeeAddressBech32: Bech32-encoded fee address
//   - FeeForwardGasLimit: Gas limit for fee forward transactions
package types
