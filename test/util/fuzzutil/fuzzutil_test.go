package fuzzutil_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/stretchr/testify/require"
)

func TestConsumerReadsBigEndian(t *testing.T) {
	c := fuzzutil.New([]byte{1, 0x01, 0x02, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 7, 3})
	require.Equal(t, 16, c.Remaining())
	require.True(t, c.Bool())
	require.Equal(t, uint16(0x0102), c.Uint16())
	require.Equal(t, uint32(0x100), c.Uint32())
	require.Equal(t, uint64(7), c.Uint64())
	require.Equal(t, byte(3), c.Byte())
	require.True(t, c.Exhausted())
}

func TestConsumerExhausted(t *testing.T) {
	c := fuzzutil.New([]byte{0xff})
	require.Equal(t, uint32(0xff000000), c.Uint32(), "short reads are zero-padded")
	require.True(t, c.Exhausted())
	require.Zero(t, c.Byte())
	require.False(t, c.Bool())
	require.Zero(t, c.Uint16())
	require.Zero(t, c.Uint64())
	require.Zero(t, c.Intn(10))
	require.Empty(t, c.Bytes(10))
	require.Equal(t, 0, c.Remaining())
	require.Equal(t, []byte{0, 0}, readAll(c.Record(2)))

	require.Zero(t, fuzzutil.New(nil).Uint64())
}

func TestConsumerIntnBounds(t *testing.T) {
	for _, n := range []int{-5, 0, 1} {
		c := fuzzutil.New([]byte{9})
		require.Zero(t, c.Intn(n))
		require.Equal(t, 1, c.Remaining(), "Intn(%d) must not read", n)
	}
	c := fuzzutil.New([]byte{255, 7, 0xff, 0xff, 0xff, 0xff})
	require.Equal(t, 255%10, c.Intn(10))
	require.Equal(t, 7, c.Intn(256))
	require.Equal(t, int(uint64(0xffffffff)%1000), c.Intn(1000))
	for b := range 256 {
		require.Less(t, fuzzutil.New([]byte{byte(b)}).Intn(3), 3)
	}
}

func TestConsumerBytes(t *testing.T) {
	c := fuzzutil.New([]byte{5, 1, 2, 3, 4, 5, 6})
	require.Equal(t, []byte{1, 2, 3}, c.Bytes(3), "capped at max")
	require.Equal(t, 3, c.Remaining())
	c = fuzzutil.New([]byte{200, 1, 2})
	require.Equal(t, []byte{1, 2}, c.Bytes(255), "capped at remaining")
	require.True(t, c.Exhausted())
	c = fuzzutil.New([]byte{1, 2})
	require.Nil(t, c.Bytes(0))
	require.Nil(t, c.Bytes(-1))
	require.Equal(t, 2, c.Remaining(), "non-positive max must not read")
}

func TestConsumerRecord(t *testing.T) {
	c := fuzzutil.New([]byte{1, 2, 3, 4, 5})
	r := c.Record(3)
	require.Equal(t, byte(1), r.Byte())
	require.Equal(t, uint32(0x02030000), r.Uint32(), "overread stays inside the record")
	require.Equal(t, 2, c.Remaining())

	short := c.Record(4)
	require.Equal(t, []byte{4, 5, 0, 0}, readAll(short), "short record is zero-padded")
	require.True(t, c.Exhausted())
	require.Equal(t, []byte{0, 0}, readAll(c.Record(2)))
	require.True(t, c.Record(0).Exhausted())
	require.True(t, c.Record(-1).Exhausted())
}

func readAll(c *fuzzutil.Consumer) []byte {
	var out []byte
	for !c.Exhausted() {
		out = append(out, c.Byte())
	}
	return out
}

func TestRecords(t *testing.T) {
	require.Equal(t, []byte{1, 0, 0, 2, 3, 0, 4, 5, 6}, fuzzutil.Records(3, []byte{1}, []byte{2, 3}, []byte{4, 5, 6}))
	require.Empty(t, fuzzutil.Records(3))
	require.Panics(t, func() { fuzzutil.Records(1, []byte{1, 2}) })
}
