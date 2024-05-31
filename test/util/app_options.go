package util

import "github.com/cosmos/cosmos-sdk/server/types"

// emptyAppOptions implements the AppOptions interface.
var _ types.AppOptions = emptyAppOptions{}

type emptyAppOptions struct{}

func (e emptyAppOptions) Get(_ string) interface{} {
	return nil
}
