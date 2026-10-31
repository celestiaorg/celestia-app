package types

import (
	"github.com/bcp-innovations/hyperlane-cosmos/util"
)

// HyperlaneKeeper defines the expected hyperlane keeper interface.
type HyperlaneKeeper interface {
	IsmRouter() *util.Router[util.InterchainSecurityModule]
}
