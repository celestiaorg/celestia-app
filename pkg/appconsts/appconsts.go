package appconsts

import (
	"bytes"

	"github.com/tendermint/tendermint/pkg/consts"
)

var NameSpacedPaddedShareBytes = bytes.Repeat([]byte{0}, consts.MsgShareSize)
