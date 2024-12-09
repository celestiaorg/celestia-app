package errors

import (
	"cosmossdk.io/errors"
)

const AppErrorsCodespace = "app"

// general application errors
var (
	ErrTxExceedsMaxSize = errors.Register(AppErrorsCodespace, 11142, "exceeds max tx size limit")
)
