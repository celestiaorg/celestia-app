package testnode

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReverseSlice(t *testing.T) {
	tests := []struct {
		input    any
		expected any
	}{
		{[]int{1, 2, 3, 4, 5}, []int{5, 4, 3, 2, 1}},
		{[]string{"a", "b", "c", "d"}, []string{"d", "c", "b", "a"}},
		{[]int{1, 2}, []int{2, 1}},
		{[]int{1}, []int{1}},
		{[]string{}, []string{}},
	}

	for _, tt := range tests {
		switch v := tt.input.(type) {
		case []int:
			// reverseSlice modifies the input slice, so we need to make a copy
			original := make([]int, len(tt.input.([]int)))
			copy(original, tt.input.([]int))
			reverseSlice(v)
			require.True(t, reflect.DeepEqual(v, tt.expected), "reverseSlice(%v) = %v, want %v", original, tt.input, tt.expected)
		case []string:
			// reverseSlice modifies the input slice, so we need to make a copy
			original := make([]string, len(tt.input.([]string)))
			copy(original, tt.input.([]string))
			reverseSlice(v)
			require.True(t, reflect.DeepEqual(v, tt.expected), "reverseSlice(%v) = %v, want %v", original, tt.input, tt.expected)
		}
	}
}

func TestReadBlockchainHeaders(t *testing.T) {
	cfg := DefaultConfig()
	cctx, rpcAddr, _ := NewNetwork(t, cfg)
	// wait for 30 blocks to be produced
	err := cctx.WaitForBlocks(30)
	require.NoError(t, err)

	// fetch headers
	headers, err := ReadBlockchainHeaders(context.Background(), rpcAddr)
	require.NoError(t, err)
	// we should have at least 30 headers
	require.True(t, len(headers) >= 30)

	// check that the headers are in ascending order, starting from 1
	i := int64(1)
	for _, header := range headers {
		got := header.Header.Height
		require.Equal(t, i, got,
			"expected height %d, got %d", i, got)
		i++
	}
}
