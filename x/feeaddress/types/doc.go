// Package types contains type definitions, message types, and constants
// for the feeaddress module.
//
// Key types:
//   - MsgPayProtocolFee: Protocol-injected message for forwarding protocol fees
//   - GenesisState: Empty genesis state for SDK compatibility
//
// Key constants:
//   - FeeAddress: The deterministic address where tokens are sent
//   - FeeAddressBech32: Bech32-encoded fee address
//   - ProtocolFeeGasLimit: Gas limit for protocol fee transactions
//
// # Stateless Module
//
// The feeaddress module keeper is stateless (no store keys). Fee deduction and
// bank transfers are handled by ProtocolFeeTerminatorDecorator in app/ante, which
// uses the bank keeper. Communication between ante handler and keeper uses
// context values (ProtocolFeeAmountContextKey).
package types
