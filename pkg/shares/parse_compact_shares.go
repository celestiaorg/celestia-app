package shares

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// parseCompactShares returns data (transactions, intermediate state roots, or
// evidence) based on the contents of rawShares and supportedShareVersions. If
// rawShares contains a share with a version that isn't present in
// supportedShareVersions, an error is returned. The returned data [][]byte does
// not have namespaces, info bytes, data length delimiter, or unit length
// delimiters and are ready to be unmarshalled.
func parseCompactShares(rawShares [][]byte, supportedShareVersions []uint8) (data [][]byte, err error) {
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

	ss := newShareStack(rawShares)
	return ss.resolve()
}

// shareStack holds variables for peel
type shareStack struct {
	shares  [][]byte
	dataLen uint64
	// data may be transactions, intermediate state roots, or evidence depending
	// on the namespace ID for this share
	data   [][]byte
	cursor int
}

func newShareStack(shares [][]byte) *shareStack {
	return &shareStack{shares: shares}
}

func (ss *shareStack) resolve() ([][]byte, error) {
	if len(ss.shares) == 0 {
		return nil, nil
	}
	infoByte, err := ParseInfoByte(ss.shares[0][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0])
	if err != nil {
		panic(err)
	}
	if !infoByte.IsSequenceStart() {
		return nil, errors.New("first share is not a message start")
	}
	err = ss.peel(ss.shares[0][appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareSequenceLengthBytes+appconsts.CompactShareReservedBytes:], true)
	return ss.data, err
}

// peel recursively parses each unit of data (either a transaction, intermediate
// state root, or evidence) and adds it to the underlying slice of data.
// delimited should be `true` if this is the start of the next unit of data (in
// other words the data contains a unitLen delimiter prefixed to the unit).
// delimited should be `false` if calling peel on an in-progress unit.
func (ss *shareStack) peel(share []byte, delimited bool) (err error) {
	if delimited {
		var unitLen uint64
		share, unitLen, err = ParseDelimiter(share)
		if err != nil {
			return err
		}
		if unitLen == 0 {
			return nil
		}
		ss.dataLen = unitLen
	}
	// safeLen describes the point in the share where it can be safely split. If
	// split beyond this point, it is possible to break apart a length
	// delimiter, which will result in incorrect share merging
	safeLen := len(share) - binary.MaxVarintLen64
	if safeLen < 0 {
		safeLen = 0
	}
	if ss.dataLen <= uint64(safeLen) {
		ss.data = append(ss.data, share[:ss.dataLen])
		share = share[ss.dataLen:]
		return ss.peel(share, true)
	}
	// add the next share to the current share to continue merging if possible
	if len(ss.shares) > ss.cursor+1 {
		ss.cursor++
		share := append(share, ss.shares[ss.cursor][appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.CompactShareReservedBytes:]...)
		return ss.peel(share, false)
	}
	// collect any remaining data
	if ss.dataLen <= uint64(len(share)) {
		ss.data = append(ss.data, share[:ss.dataLen])
		share = share[ss.dataLen:]
		return ss.peel(share, true)
	}
	return errors.New("failure to parse block data: transaction length exceeded data length")
}
