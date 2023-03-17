package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
)

// ShareSequence represents a contiguous sequence of shares that are part of the
// same namespace and blob. For compact shares, one share sequence exists per
// reserved namespace. For sparse shares, one share sequence exists per blob.
type ShareSequence struct {
	NamespaceID namespace.ID
	Shares      []Share
}

// RawData returns the raw share data of this share sequence. The raw data does
// not contain the namespace ID, info byte, sequence length, or reserved bytes.
func (s ShareSequence) RawData() (data []byte, err error) {
	for _, share := range s.Shares {
		raw, err := share.RawData()
		if err != nil {
			return []byte{}, err
		}
		data = append(data, raw...)
	}

	sequenceLen, err := s.SequenceLen()
	if err != nil {
		return []byte{}, err
	}
	// trim any padding that may have been added to the last share
	return data[:sequenceLen], nil
}

func (s ShareSequence) SequenceLen() (uint32, error) {
	if len(s.Shares) == 0 {
		return 0, fmt.Errorf("invalid sequence length because share sequence %v has no shares", s)
	}
	firstShare := s.Shares[0]
	return firstShare.SequenceLen()
}

// validSequenceLen extracts the sequenceLen written to the first share
// and returns an error if the number of shares needed to store a sequence of
// length sequenceLen doesn't match the number of shares in this share
// sequence. Returns nil if there is no error.
func (s ShareSequence) validSequenceLen() error {
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

// numberOfSharesNeeded extracts the sequenceLen written to the share
// firstShare and returns the number of shares needed to store a sequence of
// that length.
func numberOfSharesNeeded(firstShare Share) (sharesUsed int, err error) {
	sequenceLen, err := firstShare.SequenceLen()
	if err != nil {
		return 0, err
	}

	if firstShare.IsCompactShare() {
		return CompactSharesNeeded(int(sequenceLen)), nil
	}
	return SparseSharesNeeded(sequenceLen), nil
}

// CompactSharesNeeded returns the number of compact shares needed to store a
// sequence of length sequenceLen. The parameter sequenceLen is the number
// of bytes of transactions or intermediate state roots in a sequence.
func CompactSharesNeeded(sequenceLen int) (sharesNeeded int) {
	if sequenceLen == 0 {
		return 0
	}

	if sequenceLen < appconsts.FirstCompactShareContentSize {
		return 1
	}

	bytesAvailable := appconsts.FirstCompactShareContentSize
	sharesNeeded++
	for bytesAvailable < sequenceLen {
		bytesAvailable += appconsts.ContinuationCompactShareContentSize
		sharesNeeded++
	}
	return sharesNeeded
}

// SparseSharesNeeded returns the number of shares needed to store a sequence of
// length sequenceLen.
func SparseSharesNeeded(sequenceLen uint32) (sharesNeeded int) {
	if sequenceLen == 0 {
		return 0
	}

	if sequenceLen < appconsts.FirstSparseShareContentSize {
		return 1
	}

	bytesAvailable := appconsts.FirstSparseShareContentSize
	sharesNeeded++
	for uint32(bytesAvailable) < sequenceLen {
		bytesAvailable += appconsts.ContinuationSparseShareContentSize
		sharesNeeded++
	}
	return sharesNeeded
}
