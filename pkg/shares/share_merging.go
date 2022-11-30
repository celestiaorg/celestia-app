package shares

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"
	coretypes "github.com/tendermint/tendermint/types"
)

// merge extracts block data from an extended data square.
func merge(eds *rsmt2d.ExtendedDataSquare) (coretypes.Data, error) {
	squareSize := eds.Width() / 2

	// sort block data shares by namespace
	var (
		sortedTxShares   [][]byte
		sortedBlobShares [][]byte
	)

	// iterate over each row index
	for x := uint(0); x < squareSize; x++ {
		// iterate over each share in the original data square
		row := eds.Row(x)

		for _, share := range row[:squareSize] {
			// sort the data of that share types via namespace
			nid := share[:appconsts.NamespaceSize]
			switch {
			case bytes.Equal(appconsts.TxNamespaceID, nid):
				sortedTxShares = append(sortedTxShares, share)

			case bytes.Equal(appconsts.TailPaddingNamespaceID, nid):
				continue

			// ignore unused but reserved namespaces
			case bytes.Compare(nid, appconsts.MaxReservedNamespace) < 1:
				continue

			// every other namespaceID should be a blob
			default:
				sortedBlobShares = append(sortedBlobShares, share)
			}
		}
	}

	// pass the raw share data to their respective parsers
	txs, err := ParseTxs(sortedTxShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	blobs, err := ParseBlobs(sortedBlobShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	return coretypes.Data{
		Txs:        txs,
		Blobs:      blobs,
		SquareSize: uint64(squareSize),
	}, nil
}

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

// ShareSequence represents a contiguous sequence of shares that are part of the
// same namespace and blob. For compact shares, one share sequence exists per
// reserved namespace. For sparse shares, one share sequence exists per blob.
type ShareSequence struct {
	NamespaceID namespace.ID
	Shares      []Share
}

func ParseShares(rawShares [][]byte) ([]ShareSequence, error) {
	sequences := []ShareSequence{}
	currentSequence := ShareSequence{}

	for _, rawShare := range rawShares {
		share, err := NewShare(rawShare)
		if err != nil {
			return sequences, err
		}
		infoByte, err := share.InfoByte()
		if err != nil {
			return sequences, err
		}
		if infoByte.IsSequenceStart() {
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
		if err := sequence.validSequenceLength(); err != nil {
			return sequences, err
		}
	}

	return sequences, nil
}

// validSequenceLength extracts the sequenceLength written to the first share
// and returns an error if the number of shares needed to store a sequence of
// length sequenceLength doesn't match the number of shares in this share
// sequence. Returns nil if there is no error.
func (s ShareSequence) validSequenceLength() error {
	if len(s.Shares) == 0 {
		return fmt.Errorf("invalid sequence length because share sequence %v has no shares", s)
	}
	firstShare := s.Shares[0]
	sharesNeeded, err := numberOfSharesNeeded(firstShare)
	if err != nil {
		return err
	}

	if len(s.Shares) != sharesNeeded {
		return fmt.Errorf("share sequence has %d shares but needed %d shares", len(s.Shares), sharesNeeded)
	}
	return nil
}

// numberOfSharesNeeded extracts the sequenceLength written to the share
// firstShare and returns the number of shares needed to store a sequence of
// that length.
func numberOfSharesNeeded(firstShare Share) (sharesUsed int, err error) {
	sequenceLength, err := firstShare.SequenceLength()
	if err != nil {
		return 0, err
	}

	if firstShare.isCompactShare() {
		return compactSharesNeeded(int(sequenceLength)), nil
	}
	return sparseSharesNeeded(int(sequenceLength)), nil
}

// compactSharesNeeded returns the number of compact shares needed to store a
// sequence of length sequenceLength. The parameter sequenceLength is the number
// of bytes of transactions or intermediate state roots in a sequence.
func compactSharesNeeded(sequenceLength int) (sharesNeeded int) {
	if sequenceLength == 0 {
		return 0
	}

	if sequenceLength < appconsts.FirstCompactShareContentSize {
		return 1
	}
	sequenceLength -= appconsts.FirstCompactShareContentSize
	sharesNeeded++

	for sequenceLength > 0 {
		sequenceLength -= appconsts.ContinuationCompactShareContentSize
		sharesNeeded++
	}
	return sharesNeeded
}

// sparseSharesNeeded returns the number of shares needed to store a sequence of
// length sequenceLength.
func sparseSharesNeeded(sequenceLength int) (sharesNeeded int) {
	sharesNeeded = sequenceLength / appconsts.SparseShareContentSize
	if sequenceLength%appconsts.SparseShareContentSize != 0 {
		sharesNeeded++
	}
	return sharesNeeded
}
