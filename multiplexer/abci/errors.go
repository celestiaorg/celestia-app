package abci

import "errors"

// ErrNoVersionFound is returned when the requested app version is newer than
// every registered embedded version, so the native app must serve it.
var ErrNoVersionFound = errors.New("no version found")

// ErrUnsupportedAppVersion is returned when the requested app version is older
// than, or in a gap between, the registered embedded versions.
var ErrUnsupportedAppVersion = errors.New("unsupported app version")
