package rsmt2d

import (
	cryptorand "crypto/rand"
	"fmt"
	"math/rand"
	"testing"
)

var (
	encodedDataDump [][]byte
	decodedDataDump [][]byte
)

func BenchmarkEncoding(b *testing.B) {
	// generate some fake data
	data := generateRandData(128, shareSize)
	for codecName, codec := range codecs {
		// For some implementations we want to ensure the encoder for this data length
		// is already cached and initialized. For this run with same sized arbitrary data.
		_, _ = codec.Encode(generateRandData(128, shareSize))
		b.Run(
			fmt.Sprintf("%s 128 shares %d", codecName, shareSize),
			func(b *testing.B) {
				for b.Loop() {
					encodedData, err := codec.Encode(data)
					if err != nil {
						b.Error(err)
					}
					encodedDataDump = encodedData
				}
			},
		)
	}
}

func generateRandData(count int, shareSize int) [][]byte {
	out := make([][]byte, count)
	for i := 0; i < count; i++ {
		randData := make([]byte, shareSize)
		_, err := cryptorand.Read(randData)
		if err != nil {
			panic(err)
		}
		out[i] = randData
	}
	return out
}

func BenchmarkDecoding(b *testing.B) {
	// generate some fake data
	for codecName, codec := range codecs {
		// For some implementations we want to ensure the encoder for this data length
		// is already cached and initialized. For this run with same sized arbitrary data.
		_, _ = codec.Decode(generateMissingData(128, shareSize, codec))

		data := generateMissingData(128, shareSize, codec)
		b.Run(
			fmt.Sprintf("%s 128 shares %d", codecName, shareSize),
			func(b *testing.B) {
				for b.Loop() {
					decodedData, err := codec.Decode(data)
					if err != nil {
						b.Error(err)
					}
					decodedDataDump = decodedData
				}
			},
		)
	}
}

func generateMissingData(count int, shareSize int, codec Codec) [][]byte {
	randData := generateRandData(count, shareSize)
	encoded, err := codec.Encode(randData)
	if err != nil {
		panic(err)
	}
	output := append(randData, encoded...)

	// remove half of the shares randomly
	for i := 0; i < (count / 2); {
		ind := rand.Intn(len(output))
		if output[ind] == nil {
			continue
		}
		output[ind] = nil
		i++
	}

	return output
}

// testCodec is a codec that is used for testing purposes.
type testCodec struct{}

func newTestCodec() Codec {
	return &testCodec{}
}

func (c *testCodec) Encode(share [][]byte) ([][]byte, error) {
	return share, nil
}

func (c *testCodec) Decode(share [][]byte) ([][]byte, error) {
	return share, nil
}

func (c *testCodec) MaxChunks() int {
	return 0
}

func (c *testCodec) Name() string {
	return "testCodec"
}

func (c *testCodec) ValidateChunkSize(_ int) error {
	return nil
}
