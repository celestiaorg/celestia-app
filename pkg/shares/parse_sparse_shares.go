package shares

import (
	"bytes"
	"fmt"

	coretypes "github.com/tendermint/tendermint/types"
)

type BlobWithLen struct {
	blob        coretypes.Blob
	sequenceLen uint32
}

// parseSparseShares iterates through rawShares and parses out individual
// blobs. It returns an error if a rawShare contains a share version that
// isn't present in supportedShareVersions.
func parseSparseShares(rawShares [][]byte, supportedShareVersions []uint8) (blobs []coretypes.Blob, err error) {
	if len(rawShares) == 0 {
		return nil, nil
	}
	blobsWithLen := make([]BlobWithLen, 0)

	shares := FromBytes(rawShares)
	for _, share := range shares {
		version, err := share.Version()
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(supportedShareVersions, []byte{version}) {
			return nil, fmt.Errorf("unsupported share version %v is not present in supported share versions %v", version, supportedShareVersions)
		}

		isStart, err := share.IsSequenceStart()
		if err != nil {
			return nil, err
		}

		if isStart {
			sequenceLen, err := share.SequenceLen()
			if err != nil {
				return nil, err
			}
			data, err := share.RawData()
			if err != nil {
				return nil, err
			}
			blob := coretypes.Blob{
				NamespaceID:  share.NamespaceID(),
				Data:         data,
				ShareVersion: version,
			}
			blobsWithLen = append(blobsWithLen, BlobWithLen{
				blob:        blob,
				sequenceLen: sequenceLen,
			})
		} else { // continuation share
			if len(blobsWithLen) == 0 {
				return nil, fmt.Errorf("continuation share %v without a sequence start share", share)
			}
			lastBlob := &blobsWithLen[len(blobsWithLen)-1]
			data, err := share.RawData()
			if err != nil {
				return nil, err
			}
			lastBlob.blob.Data = append(lastBlob.blob.Data, data...)
		}
	}
	for _, blobWithLen := range blobsWithLen {
		// trim any padding
		blobWithLen.blob.Data = blobWithLen.blob.Data[:blobWithLen.sequenceLen]
		if !isNamespacedPadding(blobWithLen) {
			blobs = append(blobs, blobWithLen.blob)
		}
	}

	return blobs, nil
}

func isNamespacedPadding(blobWithLen BlobWithLen) bool {
	return blobWithLen.sequenceLen == 0
}
