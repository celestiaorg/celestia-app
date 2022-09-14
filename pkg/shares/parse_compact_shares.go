package shares

import (
	"encoding/binary"
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// parseCompactShares takes raw shares and extracts out transactions,
// intermediate state roots, or evidence. The returned [][]byte do not have
// namespaces or length delimiters and are ready to be unmarshalled
func parseCompactShares(shares [][]byte) (txs [][]byte, err error) {
	if len(shares) == 0 {
		return nil, nil
	}

	ss := newShareStack(shares)
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
	err := ss.peel(ss.shares[0][appconsts.NamespaceSize+appconsts.ShareReservedBytes:], true)
	return ss.data, err
}

// peel recursively parses each chunk of data (either a transaction,
// intermediate state root, or evidence) and adds it to the underlying slice of data.
func (ss *shareStack) peel(share []byte, delimited bool) (err error) {
	if delimited {
		var txLen uint64
		share, txLen, err = ParseDelimiter(share)
		if err != nil {
			return err
		}
		if txLen == 0 {
			return nil
		}
		ss.dataLen = txLen
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
		share := append(share, ss.shares[ss.cursor][appconsts.NamespaceSize+appconsts.ShareReservedBytes:]...)
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
