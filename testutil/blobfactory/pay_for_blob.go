package blobfactory

import (
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

var defaultSigner = testutil.RandomAddress().String()

func RandMsgPayForBlobWithSigner(singer string, size int) (*blobtypes.MsgPayForBlob, []byte) {
	blob := tmrand.Bytes(size)
	msg, err := blobtypes.NewMsgPayForBlob(
		singer,
		namespace.RandomBlobNamespace(),
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func RandMsgPayForBlob(size int) (*blobtypes.MsgPayForBlob, []byte) {
	blob := tmrand.Bytes(size)
	msg, err := blobtypes.NewMsgPayForBlob(
		defaultSigner,
		namespace.RandomBlobNamespace(),
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}
