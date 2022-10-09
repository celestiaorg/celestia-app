package shares

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// NewReservedBytes returns a byte slice of length
// appconsts.CompactShareReservedBytes that contains a varint of the byteIndex
// of the first unit that starts in a compact share. If no unit starts in the
// compact share, ReservedBytes is [0, 0].
func NewReservedBytes(byteIndex uint64) ([]byte, error) {
	if byteIndex >= appconsts.ShareSize {
		return []byte{}, fmt.Errorf("byte index %d must be less than share size %d", byteIndex, appconsts.ShareSize)
	}
	reservedBytes := make([]byte, appconsts.CompactShareReservedBytes)
	binary.PutUvarint(reservedBytes, byteIndex)
	return reservedBytes, nil
}

// ParseReservedBytes parses a byte slice of length
// appconsts.CompactShareReservedBytes into a byteIndex.
func ParseReservedBytes(reservedBytes []byte) (uint64, error) {
	if len(reservedBytes) != appconsts.CompactShareReservedBytes {
		return 0, fmt.Errorf("reserved bytes must be of length %d", appconsts.CompactShareReservedBytes)
	}
	reader := bytes.NewReader(reservedBytes)
	byteIndex, err := binary.ReadUvarint(reader)
	if err != nil {
		return 0, err
	}
	if byteIndex >= appconsts.ShareSize {
		return 0, fmt.Errorf("reserved bytes varint %d must be less than share size %d", byteIndex, appconsts.ShareSize)
	}
	return byteIndex, nil
}
