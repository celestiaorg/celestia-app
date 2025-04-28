package abci

import "errors"

// ErrNoVersionFound is returned when no remote version is found for a given app version.
var ErrNoVersionFound = errors.New("no version found")
