package testfactory

import (
	"bytes"
	"encoding/binary"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/test/util/random"
	"github.com/celestiaorg/go-square/v4/share"
)

func GenerateRandomBlob(dataSize int) *share.Blob {
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize))
	blob, err := share.NewBlob(ns, random.Bytes(dataSize), appconsts.DefaultShareVersion, nil)
	if err != nil {
		panic(err)
	}
	return blob
}

// DelimLen calculates the length of the delimiter for a given unit size
func DelimLen(size uint64) int {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(lenBuf, size)
}
