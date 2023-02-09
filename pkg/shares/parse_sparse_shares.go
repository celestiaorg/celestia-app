package shares

import (
	"bytes"
	"fmt"

	coretypes "github.com/tendermint/tendermint/types"
)

type sequence struct {
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
	sequences := make([]sequence, 0)

	shares := FromBytes(rawShares)
	for _, share := range shares {
		version, err := share.Version()
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(supportedShareVersions, []byte{version}) {
			return nil, fmt.Errorf("unsupported share version %v is not present in supported share versions %v", version, supportedShareVersions)
		}

		isPadding, err := share.IsPadding()
		if err != nil {
			return nil, err
		}
		if isPadding {
			continue
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
			sequences = append(sequences, sequence{
				blob:        blob,
				sequenceLen: sequenceLen,
			})
		} else { // continuation share
			if len(sequences) == 0 {
				return nil, fmt.Errorf("continuation share %v without a sequence start share", share)
			}
			prev := &sequences[len(sequences)-1]
			data, err := share.RawData()
			if err != nil {
				return nil, err
			}
			prev.blob.Data = append(prev.blob.Data, data...)
		}
	}
	for _, sequence := range sequences {
		// trim any padding from the end of the sequence
		sequence.blob.Data = sequence.blob.Data[:sequence.sequenceLen]
		blobs = append(blobs, sequence.blob)
	}

	return blobs, nil
}
