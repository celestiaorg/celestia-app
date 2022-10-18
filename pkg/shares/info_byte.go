package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// InfoByte is a byte with the following structure: the first 7 bits are
// reserved for version information in big endian form (initially `0000000`).
// The last bit is a "sequence start indicator", that is `1` if this is the
// first share of a sequence and `0` if this is a continuation share.
type InfoByte byte

func NewInfoByte(version uint8, isSequenceStart bool) (InfoByte, error) {
	if version > appconsts.MaxShareVersion {
		return 0, fmt.Errorf("version %d must be less than or equal to %d", version, appconsts.MaxShareVersion)
	}

	prefix := version << 1
	if isSequenceStart {
		return InfoByte(prefix + 1), nil
	}
	return InfoByte(prefix), nil
}

// Version returns the version encoded in this InfoByte. Version is
// expected to be between 0 and appconsts.MaxShareVersion (inclusive).
func (i InfoByte) Version() uint8 {
	version := uint8(i) >> 1
	return version
}

// IsSequenceStart returns whether this share is the start of a message.
func (i InfoByte) IsSequenceStart() bool {
	return uint(i)%2 == 1
}

func ParseInfoByte(i byte) (InfoByte, error) {
	isSequenceStart := i%2 == 1
	version := uint8(i) >> 1
	return NewInfoByte(version, isSequenceStart)
}
