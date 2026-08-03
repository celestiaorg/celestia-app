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

	// ErrInvalidPayForFibreTx is returned when MsgPayForFibre is not the only tx message.
	ErrInvalidPayForFibreTx = errors.Register(AppErrorsCodespace, 11144, "MsgPayForFibre must be the only message in a transaction")

	// ErrMultiMsgIndexWrapper is returned when an IndexWrapper does not contain exactly one SDK message.
	ErrMultiMsgIndexWrapper = errors.Register(AppErrorsCodespace, 11145, "IndexWrapper must contain a single SDK message")

	// ErrNonPFBIndexWrapper is returned when an IndexWrapper contains a message other than MsgPayForBlobs.
	ErrNonPFBIndexWrapper = errors.Register(AppErrorsCodespace, 11146, "IndexWrapper must contain MsgPayForBlobs")
)
