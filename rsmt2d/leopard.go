package rsmt2d

import (
	"fmt"
	"sync"

	"github.com/klauspost/reedsolomon"
)

var _ Codec = &LeoRSCodec{}

func init() {
	registerCodec(Leopard, NewLeoRSCodec())
}

type LeoRSCodec struct {
	// Cache the encoders of various sizes to not have to re-instantiate those
	// as it is costly.
	//
	// Note that past sizes are not removed from the cache at all as the various
	// data sizes are expected to relatively small and will not cause any memory issue.
	//
	// TODO: switch to a generic version of sync.Map with type reedsolomon.Encoder
	// once it made it into the standard lib
	encCache sync.Map
}

func (l *LeoRSCodec) Encode(data [][]byte) ([][]byte, error) {
	dataLen := len(data)
	enc, err := l.loadOrInitEncoder(dataLen)
	if err != nil {
		return nil, err
	}

	shares := make([][]byte, dataLen*2)
	copy(shares, data)
	for i := dataLen; i < len(shares); i++ {
		shares[i] = make([]byte, len(data[0]))
	}

	if err := enc.Encode(shares); err != nil {
		return nil, err
	}
	return shares[dataLen:], nil
}

// Decode reconstructs missing shares from the provided data. Missing shares
// must be nil. The returned slice is the same slice as data: Reconstruct fills
// the nil entries in place, so callers that need to retain the original
// (nil-preserving) input must copy it before calling Decode.
func (l *LeoRSCodec) Decode(data [][]byte) ([][]byte, error) {
	half := len(data) / 2
	enc, err := l.loadOrInitEncoder(half)
	if err != nil {
		return nil, err
	}
	err = enc.Reconstruct(data)
	return data, err
}

func (l *LeoRSCodec) loadOrInitEncoder(dataLen int) (reedsolomon.Encoder, error) {
	enc, ok := l.encCache.Load(dataLen)
	if !ok {
		var err error
		enc, err = reedsolomon.New(dataLen, dataLen, reedsolomon.WithLeopardGF(true))
		if err != nil {
			return nil, err
		}
		l.encCache.Store(dataLen, enc)
	}
	return enc.(reedsolomon.Encoder), nil
}

// MaxChunks returns the max number of shares this codec supports in a 2D
// original data square.
func (l *LeoRSCodec) MaxChunks() int {
	// klauspost/reedsolomon supports an EDS width of 65536. See:
	// https://github.com/klauspost/reedsolomon/blob/523164698be98f1603cf1235f5a1de17728b2091/leopard.go#L42C31-L42C36
	maxEDSWidth := 65536
	// An EDS width of 65536 is an ODS width of 32768.
	maxODSWidth := maxEDSWidth / 2
	// The max number of shares in a 2D original data square is 32768 * 32768.
	return maxODSWidth * maxODSWidth
}

func (l *LeoRSCodec) Name() string {
	return Leopard
}

// ValidateChunkSize returns an error if this codec does not support
// shareSize. Returns nil if shareSize is supported.
func (l *LeoRSCodec) ValidateChunkSize(shareSize int) error {
	// See https://github.com/catid/leopard/blob/22ddc7804998d31c8f1a2617ee720e063b1fa6cd/README.md?plain=1#L27
	// See https://github.com/klauspost/reedsolomon/blob/fd3e6910a7e457563469172968f456ad9b7696b6/README.md?plain=1#L403
	if shareSize%64 != 0 {
		return fmt.Errorf("shareSize %v must be a multiple of 64 bytes", shareSize)
	}
	return nil
}

func NewLeoRSCodec() *LeoRSCodec {
	return &LeoRSCodec{}
}
