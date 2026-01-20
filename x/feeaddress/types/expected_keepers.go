package types

// This module intentionally has no keeper dependencies.
//
// Design rationale:
// - The feeaddress keeper is stateless - it has no store keys or persistent state
// - Fee deduction and bank transfers are handled by the FeeForwardDecorator in app/ante
// - Communication between the ante handler and keeper happens via context values:
//   - FeeForwardContextKey: Set early to flag the tx as a fee forward tx
//   - FeeForwardAmountContextKey: Set after fee deduction to provide the amount for event emission
//
// This design keeps the module minimal and avoids circular dependencies between
// the keeper and ante handler components.
