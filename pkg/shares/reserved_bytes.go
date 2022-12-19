package shares

import (
	"encoding/binary"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// NewReservedBytes returns a byte slice of length
// appconsts.CompactShareReservedBytes that contains the byteIndex of the first
// unit that starts in a compact share.
func NewReservedBytes(byteIndex uint32) ([]byte, error) {
	if byteIndex >= appconsts.ShareSize {
		return []byte{}, fmt.Errorf("byte index %d must be less than share size %d", byteIndex, appconsts.ShareSize)
	}
	reservedBytes := make([]byte, appconsts.CompactShareReservedBytes)
	binary.BigEndian.PutUint32(reservedBytes, byteIndex)
	return reservedBytes, nil
}

// ParseReservedBytes parses a byte slice of length
// appconsts.CompactShareReservedBytes into a byteIndex.
func ParseReservedBytes(reservedBytes []byte) (uint32, error) {
	if len(reservedBytes) != appconsts.CompactShareReservedBytes {
		return 0, fmt.Errorf("reserved bytes must be of length %d", appconsts.CompactShareReservedBytes)
	}
	byteIndex := binary.BigEndian.Uint32(reservedBytes)
	if appconsts.ShareSize <= byteIndex {
		return 0, fmt.Errorf("byteIndex must be less than share size %d", appconsts.ShareSize)
	}
	return byteIndex, nil
}
