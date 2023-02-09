package shares

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	coretypes "github.com/tendermint/tendermint/types"
)

// ParseTxs collects all of the transactions from the shares provided
func ParseTxs(shares [][]byte) (coretypes.Txs, error) {
	// parse the sharse
	rawTxs, err := parseCompactShares(shares, appconsts.SupportedShareVersions)
	if err != nil {
		return nil, err
	}

	// convert to the Tx type
	txs := make(coretypes.Txs, len(rawTxs))
	for i := 0; i < len(txs); i++ {
		txs[i] = coretypes.Tx(rawTxs[i])
	}

	return txs, nil
}

// ParseBlobs collects all blobs from the shares provided
func ParseBlobs(shares [][]byte) ([]coretypes.Blob, error) {
	blobList, err := parseSparseShares(shares, appconsts.SupportedShareVersions)
	if err != nil {
		return []coretypes.Blob{}, err
	}

	return blobList, nil
}

func ParseShares(rawShares [][]byte) ([]ShareSequence, error) {
	sequences := []ShareSequence{}
	currentSequence := ShareSequence{}

	for _, rawShare := range rawShares {
		share, err := NewShare(rawShare)
		if err != nil {
			return sequences, err
		}
		isStart, err := share.IsSequenceStart()
		if err != nil {
			return sequences, err
		}
		if isStart {
			if len(currentSequence.Shares) > 0 {
				sequences = append(sequences, currentSequence)
			}
			currentSequence = ShareSequence{
				Shares:      []Share{share},
				NamespaceID: share.NamespaceID(),
			}
		} else {
			if !bytes.Equal(currentSequence.NamespaceID, share.NamespaceID()) {
				return sequences, fmt.Errorf("share sequence %v has inconsistent namespace IDs with share %v", currentSequence, share)
			}
			currentSequence.Shares = append(currentSequence.Shares, share)
		}
	}

	if len(currentSequence.Shares) > 0 {
		sequences = append(sequences, currentSequence)
	}

	for _, sequence := range sequences {
		if err := sequence.validSequenceLen(); err != nil {
			return sequences, err
		}
	}

	return sequences, nil
}
