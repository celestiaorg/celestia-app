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
//
// # Stateless Module
//
// The feeaddress keeper is stateless (no store keys or keeper dependencies).
// Fee deduction and bank transfers are handled by FeeForwardTerminatorDecorator
// in app/ante, which requires the bank keeper. Communication between ante handler
// and keeper uses context values (FeeForwardAmountContextKey).
package types
