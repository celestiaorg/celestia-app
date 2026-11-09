package appconsts

// These constants are in a separate file from fibre_consts.go because they are
// referenced by both benchmark and non-benchmark builds.
// fibre_consts.go is gated behind //go:build !benchmarks, so any constants
// there are unavailable in benchmark builds.
const (
	// PFBFibreGasFixedCost is the fixed gas cost per blob in a PayForFibre transaction.
	PFBFibreGasFixedCost uint64 = 650_000

	// PFBFibreGasPerChunk is the gas cost per 256 KiB chunk in a PayForFibre transaction.
	PFBFibreGasPerChunk uint64 = 45_000

	// PFBFibreChunkSize is the chunk size (256 KiB) used for gas calculation in PayForFibre.
	PFBFibreChunkSize uint32 = 262_144

	// PFFibreTxGasFixedCost is the fixed gas for MsgPayForFibre validation.
	// PFFibreTxGasFixedCost is the fixed gas for MsgPayForFibre validation.
	// Set to the pre-#7599 metered cost of loading the validator set
	// (measured on a 25/100-validator talis network: 1,378 fixed plus 828
	// for each validator record read; signature crypto was never metered).
	PFFibreTxGasFixedCost uint64 = 1_378

	// PFFibreGasPerValidatorSignature is the gas per validator signature.
	// 828 for state reads + crypto.
	PFFibreGasPerValidatorSignature uint64 = 1_000

	// PayForFibreSettlementGas is the fixed gas a MsgPayForFibre settlement
	// charges in place of metered store access, so its gas does not depend on
	// state that earlier txs in the block change. Covers the ~35-40k metered
	// cost of settling against an escrow with no pending withdrawals.
	PayForFibreSettlementGas uint64 = 45_000

	// PayForFibreGasPerWithdrawal is the gas a MsgPayForFibre settlement
	// charges for each pending withdrawal it reduces or deletes to cover a
	// shortfall in the escrow's available balance.
	PayForFibreGasPerWithdrawal uint64 = 5_000

	// MaxFibreValidatorSignatures is a stateless ceiling on a MsgPayForFibre's
	// signatures; keep it >= MaxValidators (the keeper enforces the exact bound).
	MaxFibreValidatorSignatures = 1_000
)
