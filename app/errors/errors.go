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

	// ErrNonPFBIndexWrapper is returned when an IndexWrapper encoded tx wraps
	// anything other than a tx containing a single MsgPayForBlobs.
	ErrNonPFBIndexWrapper = errors.Register(AppErrorsCodespace, 11144, "index wrapper transactions must contain exactly one MsgPayForBlobs message")
)
