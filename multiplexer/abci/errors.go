package abci

import "errors"

// ErrNoVersionFound is returned when the requested app version is newer than
// every registered embedded version. Callers treat it as a signal to use the
// native (latest) app.
var ErrNoVersionFound = errors.New("no version found")

// ErrUnsupportedAppVersion is returned when the requested app version is older
// than, or falls in a gap between, the registered embedded versions. No binary
// can serve it, so callers must fail instead of guessing.
var ErrUnsupportedAppVersion = errors.New("unsupported app version")
