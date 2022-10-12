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
		panic(fmt.Sprintf("share %s is too short to contain an info byte", s))
	}
	// the info byte is the first byte after the namespace ID
	unparsed := s[appconsts.NamespaceSize]
	return ParseInfoByte(unparsed)
}

func (s Share) MessageLength() (uint64, error) {
	infoByte, err := s.InfoByte()
	if err != nil {
		return 0, err
	}
	if !infoByte.IsMessageStart() {
		return 0, nil
	}
	if s.isCompactShare() || len(s) < appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareSequenceLengthBytes {
		panic(fmt.Sprintf("compact share %s is too short to contain message length", s))
	}
	reader := bytes.NewReader(s[appconsts.NamespaceSize+appconsts.ShareInfoBytes:])
	return binary.ReadUvarint(reader)
}

// isCompactShare returns true if this share is a compact share.
func (s Share) isCompactShare() bool {
	return s.NamespaceID().Equal(appconsts.TxNamespaceID) || s.NamespaceID().Equal(appconsts.EvidenceNamespaceID)
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
