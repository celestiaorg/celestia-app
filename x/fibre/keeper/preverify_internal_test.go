package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerificationChunkSize(t *testing.T) {
	tests := map[string]struct {
		items, procs, want int
	}{
		"CheckTx's promise and certificate": {items: 2, procs: 16, want: 1},
		"one item per proc":                 {items: 16, procs: 16, want: 1},
		"a few items per proc":              {items: 40, procs: 16, want: 3},
		"a 100-PFF block":                   {items: 200, procs: 16, want: maxChunkSize},
		"a full block":                      {items: 5800, procs: 16, want: maxChunkSize},
		"one proc, a few items":             {items: 2, procs: 1, want: 2},
		"one proc, many items":              {items: 15, procs: 1, want: maxChunkSize},
		"no items":                          {items: 0, procs: 16, want: 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, verificationChunkSize(tc.items, tc.procs))
		})
	}
}
