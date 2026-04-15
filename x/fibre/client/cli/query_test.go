package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeHexHash(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		want      []byte
		wantError bool
	}{
		{
			name:  "valid hex with 0x prefix",
			input: "0xdeadbeef",
			want:  []byte{0xde, 0xad, 0xbe, 0xef},
		},
		{
			name:  "valid hex without 0x prefix",
			input: "deadbeef",
			want:  []byte{0xde, 0xad, 0xbe, 0xef},
		},
		{
			name:  "empty string",
			input: "",
			want:  []byte{},
		},
		{
			name:  "0x prefix only",
			input: "0x",
			want:  []byte{},
		},
		{
			name:  "valid 32-byte SHA-256 hash",
			input: "0x0000000000000000000000000000000000000000000000000000000000000001",
			want: []byte{
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 0,
				0, 0, 0, 0, 0, 0, 0, 1,
			},
		},
		{
			name:      "invalid hex characters",
			input:     "not-hex",
			wantError: true,
		},
		{
			name:      "odd length hex",
			input:     "0xabc",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeHexHash(tc.input)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
