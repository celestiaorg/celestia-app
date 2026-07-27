package errors

import (
	"cosmossdk.io/errors"
)

// AppErrorsCodespace defines the unique identifier of the application error space
// used to separate app-specific errors from other modules
const AppErrorsCodespace = "app"

// Application error codes start from 11000 to avoid conflicts with other modules
var (
	// ErrTxExceedsMaxSize is returned when a transaction size exceeds the maximum allowed limit
	ErrTxExceedsMaxSize = errors.Register(AppErrorsCodespace, 11142, "transaction size exceeds maximum allowed limit")

	// ErrTxExceedsMaxSDKMessages is returned when an SDK tx contains more messages than a single block may ever include.
	ErrTxExceedsMaxSDKMessages = errors.Register(AppErrorsCodespace, 11143, "transaction exceeds maximum allowed SDK message count")

	// ErrInvalidPayForFibreTx is returned when a tx contains a MsgPayForFibre alongside other messages.
	// Such txs can never be included in a valid block, so they are rejected at CheckTx.
	ErrInvalidPayForFibreTx = errors.Register(AppErrorsCodespace, 11144, "MsgPayForFibre must be the only message in a transaction")
)
