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

	// ErrDuplicateTxRawField is returned when a transaction duplicates a top-level TxRaw field that ADR-027 requires to be unique.
	ErrDuplicateTxRawField = errors.Register(AppErrorsCodespace, 11144, "transaction contains a duplicate TxRaw field")
)
