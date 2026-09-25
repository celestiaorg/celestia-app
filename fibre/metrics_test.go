package fibre

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSizeBucket(t *testing.T) {
	cases := map[int64]int64{
		-1:        0,
		0:         0,
		1:         1,
		2:         2,
		3:         4,
		4:         4,
		5:         8,
		1024:      1024,
		1025:      2048,
		1 << 27:   1 << 27,
		1<<27 + 1: 1 << 28,
	}
	for size, want := range cases {
		require.Equal(t, want, sizeBucket(size), "size %d", size)
	}
}
