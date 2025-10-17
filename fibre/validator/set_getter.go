package validator

import (
	"context"
)

// SetGetter defines an interface for retrieving [Set].
type SetGetter interface {
	// Head returns the latest [Set].
	Head(context.Context) (Set, error)

	// GetByHeight returns the [Set] at the given height.
	// Height must be greater than 0.
	GetByHeight(context.Context, uint64) (Set, error)
}
