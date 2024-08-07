package utils

import (
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

// NoopAppOptions implements the AppOptions interface.
var _ servertypes.AppOptions = (*NoopAppOptions)(nil)

// NoopAppOptions is a no-op implementation of servertypes.AppOptions.
type NoopAppOptions struct{}

func (nao NoopAppOptions) Get(string) interface{} {
	return nil
}
