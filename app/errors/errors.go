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
)
