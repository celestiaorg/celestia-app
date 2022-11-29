package shares

import (
	"bytes"
	"fmt"

	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// parseSparseShares iterates through rawShares and parses out individual
// blobs. It returns an error if a rawShare contains a share version that
// isn't present in supportedShareVersions.
func parseSparseShares(rawShares [][]byte, supportedShareVersions []uint8) ([]coretypes.Blob, error) {
	if len(rawShares) == 0 {
		return nil, nil
	}
	shares := FromBytes(rawShares)
	for _, share := range shares {
		infoByte, err := share.InfoByte()
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(supportedShareVersions, []byte{infoByte.Version()}) {
			return nil, fmt.Errorf("unsupported share version %v is not present in the list of supported share versions %v", infoByte.Version(), supportedShareVersions)
		}
	}

	// blobs returned
	blobs := []coretypes.Blob{}
	currentBlobLen := 0
	currentBlob := coretypes.Blob{}
	// whether the current share contains the start of a new blob
	isNewBlob := true
	// the len in bytes of the current chunk of data that will eventually become
	// a blob. This is identical to len(currentBlob.Data) + appconsts.BlobShareSize
	// but we cache it here for readability
	dataLen := 0
	saveBlob := func() {
		blobs = append(blobs, currentBlob)
		dataLen = 0
		isNewBlob = true
	}
	// iterate through all the shares and parse out each blob
	for i := 0; i < len(rawShares); i++ {
		dataLen = len(currentBlob.Data) + appconsts.SparseShareContentSize
		switch {
		case isNewBlob:
			nextBlobChunk, nextBlobLen, err := ParseDelimiter(rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:])
			if err != nil {
				return nil, err
			}
			// the current share is namespaced padding so we ignore it
			if bytes.Equal(rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:], appconsts.NameSpacedPaddedShareBytes) {
				continue
			}
			currentBlobLen = int(nextBlobLen)
			nid := rawShares[i][:appconsts.NamespaceSize]
			infoByte, err := ParseInfoByte(rawShares[i][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0])
			if err != nil {
				panic(err)
			}
			if infoByte.IsSequenceStart() != isNewBlob {
				return nil, fmt.Errorf("expected sequence start indicator to be %t but got %t", isNewBlob, infoByte.IsSequenceStart())
			}
			currentBlob = coretypes.Blob{
				NamespaceID: nid,
				Data:        nextBlobChunk,
				// TODO: add the share version to the blob
				// https://github.com/celestiaorg/celestia-app/issues/1053
			}
			// the current share contains the entire blob so we save it and
			// progress
			if currentBlobLen <= len(nextBlobChunk) {
				currentBlob.Data = currentBlob.Data[:currentBlobLen]
				saveBlob()
				continue
			}
			isNewBlob = false
		// this entire share contains a chunk of blob that we need to save
		case currentBlobLen > dataLen:
			currentBlob.Data = append(currentBlob.Data, rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:]...)
		// this share contains the last chunk of data needed to complete the
		// blob
		case currentBlobLen <= dataLen:
			remaining := currentBlobLen - len(currentBlob.Data) + appconsts.NamespaceSize + appconsts.ShareInfoBytes
			currentBlob.Data = append(currentBlob.Data, rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:remaining]...)
			saveBlob()
		}
	}
	return blobs, nil
}
