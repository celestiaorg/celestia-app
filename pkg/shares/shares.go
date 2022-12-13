package shares

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
)

// Share contains the raw share data (including namespace ID).
type Share []byte

func NewShare(data []byte) (Share, error) {
	if len(data) != appconsts.ShareSize {
		return nil, fmt.Errorf("share data must be %d bytes, got %d", appconsts.ShareSize, len(data))
	}
	return Share(data), nil
}

func (s Share) NamespaceID() namespace.ID {
	if len(s) < appconsts.NamespaceSize {
		panic(fmt.Sprintf("share %s is too short to contain a namespace ID", s))
	}
	return namespace.ID(s[:appconsts.NamespaceSize])
}

func (s Share) InfoByte() (InfoByte, error) {
	if len(s) < appconsts.NamespaceSize+appconsts.ShareInfoBytes {
		return 0, fmt.Errorf("share %s is too short to contain an info byte", s)
	}
	// the info byte is the first byte after the namespace ID
	unparsed := s[appconsts.NamespaceSize]
	return ParseInfoByte(unparsed)
}

// SequenceLen returns the value of the sequence length varint, the number of
// bytes occupied by the sequence length varint, and optionally an error. It
// returns 0, 0, nil if this is a continuation share (i.e. doesn't contain a
// sequence length).
func (s Share) SequenceLen() (len uint64, numBytes int, err error) {
	isSequenceStart, err := s.isSequenceStart()
	if err != nil {
		return 0, 0, err
	}
	if !isSequenceStart {
		return 0, 0, nil
	}

	reader := bytes.NewReader(s[appconsts.NamespaceSize+appconsts.ShareInfoBytes:])
	len, err = binary.ReadUvarint(reader)
	if err != nil {
		return 0, 0, err
	}

	if s.isCompactShare() {
		return len, appconsts.FirstCompactShareSequenceLengthBytes, nil
	}
	return len, numberOfBytesVarint(len), nil
}

// RawData returns the raw share data. The raw share data does not contain the
// namespace ID, info byte, or sequence length. It does contain the reserved
// bytes for compact shares.
func (s Share) RawData() (rawData []byte, err error) {
	_, numSequenceLengthBytes, err := s.SequenceLen()
	if err != nil {
		return rawData, err
	}

	rawDataStartIndex := appconsts.NamespaceSize + appconsts.ShareInfoBytes + numSequenceLengthBytes
	if len(s) < rawDataStartIndex {
		return rawData, fmt.Errorf("share %s is too short to contain raw data", s)
	}

	return s[rawDataStartIndex:], nil
}

func (s Share) isSequenceStart() (bool, error) {
	infoByte, err := s.InfoByte()
	if err != nil {
		return false, err
	}
	return infoByte.IsSequenceStart(), nil
}

// isCompactShare returns true if this share is a compact share.
func (s Share) isCompactShare() bool {
	return s.NamespaceID().Equal(appconsts.TxNamespaceID)
}

func ToBytes(shares []Share) (bytes [][]byte) {
	bytes = make([][]byte, len(shares))
	for i, share := range shares {
		bytes[i] = []byte(share)
	}
	return bytes
}

func FromBytes(bytes [][]byte) (shares []Share) {
	shares = make([]Share, len(bytes))
	for i, b := range bytes {
		shares[i] = Share(b)
	}
	return shares
}

// numberOfBytesVarint calculates the number of bytes needed to write a varint of n
func numberOfBytesVarint(n uint64) (numberOfBytes int) {
	buf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(buf, n)
}
