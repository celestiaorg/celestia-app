package shares

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseReservedBytes(t *testing.T) {
	type testCase struct {
		name      string
		input     []byte
		want      uint64
		expectErr bool
	}
	testCases := []testCase{
		{"byte index of 0", []byte{0, 0}, 0, false},
		{"byte index of 2", []byte{2, 0}, 2, false},
		{"byte index of 4", []byte{4, 0}, 4, false},
		{"byte index of 8", []byte{8, 0}, 8, false},
		{"byte index of 16", []byte{16, 0}, 16, false},
		{"byte index of 32", []byte{32, 0}, 32, false},
		{"byte index of 64", []byte{64, 0}, 64, false},
		{"byte index of 128", []byte{128, 1}, 128, false},
		{"byte index of 256", []byte{128, 2}, 256, false},
		{"byte index of 511", []byte{255, 3}, 511, false},

		// error cases
		{"empty", []byte{}, 0, true},
		{"too few reserved bytes", []byte{1}, 0, true},
		{"too many reserved bytes", []byte{3, 3, 3}, 0, true},
		{"byte index of 512 is equal to share size", []byte{128, 4}, 0, true},
		{"byte index of 1000 is greater than share size", []byte{232, 7}, 0, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseReservedBytes(tc.input)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNewReservedBytes(t *testing.T) {
	type testCase struct {
		name      string
		input     uint64
		want      []byte
		expectErr bool
	}
	testCases := []testCase{
		{"byte index of 0", 0, []byte{0, 0}, false},
		{"byte index of 2", 2, []byte{2, 0}, false},
		{"byte index of 4", 4, []byte{4, 0}, false},
		{"byte index of 8", 8, []byte{8, 0}, false},
		{"byte index of 16", 16, []byte{16, 0}, false},
		{"byte index of 32", 32, []byte{32, 0}, false},
		{"byte index of 64", 64, []byte{64, 0}, false},
		{"byte index of 128", 128, []byte{128, 1}, false},
		{"byte index of 256", 256, []byte{128, 2}, false},
		{"byte index of 511", 511, []byte{255, 3}, false},

		// error cases
		{"byte index of 512 is equal to share size", 512, []byte{}, true},
		{"byte index of 1000 is greater than share size", 1000, []byte{}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewReservedBytes(tc.input)
			if tc.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
