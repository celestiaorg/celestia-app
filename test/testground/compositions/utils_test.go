package compositions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBandwidth(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
		err      bool
	}{
		{"Valid Kib", "10Kib", 10 * (1 << 10), false},
		{"Valid Mib", "5Mib", 5 * (1 << 20), false},
		{"Valid Gib", "2Gib", 2 * (1 << 30), false},
		{"Valid Tib", "1Tib", 1 * (1 << 40), false},
		{"Valid Kb", "100Kb", 100_000, false},
		{"Valid Mb", "3Mb", 3_000_000, false},
		{"Valid Gb", "7Gb", 7_000_000_000, false},
		{"Valid GB", "1GB", 1_000_000_000, false},
		{"Valid Tb", "1Tb", 1_000_000_000_000, false},
		{"Invalid Unit", "10Xb", 0, true},
		{"Invalid Format", "abc", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseBandwidth(tc.input)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, result)
			}
		})
	}
}
