package appconsts

import (
	"bytes"

	"github.com/tendermint/tendermint/pkg/consts"
)

// MaxShareVersion is the maximum value a share version can be.
const MaxShareVersion = 127

var NameSpacedPaddedShareBytes = bytes.Repeat([]byte{0}, consts.MsgShareSize)
