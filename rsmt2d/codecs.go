package rsmt2d

import "fmt"

const (
	// Leopard is a codec that was originally implemented in the C++ library
	// https://github.com/catid/leopard. rsmt2d uses a Go port of the C++
	// library in https://github.com/klauspost/reedsolomon. The Leopard codec
	// uses 8-bit leopard for shares less than or equal to 256. The Leopard
	// codec uses 16-bit leopard for shares greater than 256.
	Leopard = "Leopard"
)

type Codec interface {
	// Encode encodes original data, automatically extracting share size.
	// There must be no missing shares. Only returns parity shares.
	Encode(data [][]byte) ([][]byte, error)
	// Decode decodes sparse original + parity data, automatically extracting share size.
	// Missing shares must be nil. Returns original + parity data.
	Decode(data [][]byte) ([][]byte, error)
	// MaxChunks returns the max number of chunks this codec supports in a 2D
	// original data square. Chunk is a synonym of share.
	MaxChunks() int
	// Name returns the name of the codec.
	Name() string
	// ValidateChunkSize returns an error if this codec does not support
	// chunkSize. Returns nil if chunkSize is supported. Chunk is a synonym of
	// share.
	ValidateChunkSize(chunkSize int) error
}

// codecs is a global map used for keeping track of registered codecs for testing and JSON unmarshalling
var codecs = make(map[string]Codec)

func registerCodec(ct string, codec Codec) {
	if codecs[ct] != nil {
		panic(fmt.Sprintf("%v already registered", codec))
	}
	codecs[ct] = codec
}
