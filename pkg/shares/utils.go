package shares

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// DelimLen calculates the length of the delimiter for a given blob size
func DelimLen(size uint64) int {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(lenBuf, size)
}

// BlobSharesUsed calculates the minimum number of shares a blob will take up.
// It accounts for the necessary delimiter and potential padding. blobSize must
// be provided in number of bytes.
func BlobSharesUsed(blobSize int) int {
	// add the delimiter to the blob size
	blobSize = DelimLen(uint64(blobSize)) + blobSize
	shareCount := blobSize / appconsts.SparseShareContentSize
	// increment the share count if the blob overflows the last counted share
	if blobSize%appconsts.SparseShareContentSize != 0 {
		shareCount++
	}
	return shareCount
}

func BlobShareCountsFromBlobs(blobs []core.Blob) []int {
	e := make([]int, len(blobs))
	for i, blob := range blobs {
		e[i] = BlobSharesUsed(len(blob.Data))
	}
	return e
}

func isPowerOf2(v uint64) bool {
	return v&(v-1) == 0 && v != 0
}

func BlobsFromProto(blobs []core.Blob) ([]coretypes.Blob, error) {
	result := make([]coretypes.Blob, len(blobs))
	for i, blob := range blobs {
		if blob.ShareVersion > math.MaxUint8 {
			return nil, fmt.Errorf("share version %d is too large to be a uint8", blob.ShareVersion)
		}
		result[i] = coretypes.Blob{
			NamespaceID:  blob.NamespaceId,
			Data:         blob.Data,
			ShareVersion: uint8(blob.ShareVersion),
		}
	}
	return result, nil
}

func TxsToBytes(txs coretypes.Txs) [][]byte {
	e := make([][]byte, len(txs))
	for i, tx := range txs {
		e[i] = []byte(tx)
	}
	return e
}

func TxsFromBytes(txs [][]byte) coretypes.Txs {
	e := make(coretypes.Txs, len(txs))
	for i, tx := range txs {
		e[i] = coretypes.Tx(tx)
	}
	return e
}

// zeroPadIfNecessary pads the share with trailing zero bytes if the provided
// share has fewer bytes than width. Returns the share unmodified if the
// len(share) is greater than or equal to width.
func zeroPadIfNecessary(share []byte, width int) (padded []byte, bytesOfPadding int) {
	oldLen := len(share)
	if oldLen >= width {
		return share, 0
	}

	missingBytes := width - oldLen
	padByte := []byte{0}
	padding := bytes.Repeat(padByte, missingBytes)
	share = append(share, padding...)
	return share, missingBytes
}

// ParseDelimiter finds and returns the length delimiter of the share provided
// while also removing the delimiter bytes from the input. ParseDelimiter
// applies to both compact and sparse shares. Input should not contain the
// namespace ID or info byte of a share.
func ParseDelimiter(input []byte) (inputWithoutLengthDelimiter []byte, dataLength uint64, err error) {
	if len(input) == 0 {
		return input, 0, nil
	}

	l := binary.MaxVarintLen64
	if len(input) < binary.MaxVarintLen64 {
		l = len(input)
	}

	delimiter, _ := zeroPadIfNecessary(input[:l], binary.MaxVarintLen64)

	// read the length of the data
	r := bytes.NewBuffer(delimiter)
	dataLen, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, 0, err
	}

	// calculate the number of bytes used by the delimiter
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, dataLen)

	// return the input without the length delimiter
	return input[n:], dataLen, nil
}
