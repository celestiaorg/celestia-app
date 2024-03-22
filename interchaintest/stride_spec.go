package interchaintest_test

import (
	"github.com/strangelove-ventures/interchaintest/v6"
)

var strideSpec = &interchaintest.ChainSpec{
	Name:          "stride",
	Version:       "v19.0.0",
	NumValidators: numValidators(),
	NumFullNodes:  numFullNodes(),
}
