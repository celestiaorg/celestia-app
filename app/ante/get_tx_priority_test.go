package ante

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/assert"
)

func TestGetTxPriority(t *testing.T) {
	cases := []struct {
		name        string
		fee         math.Int
		gas         uint64
		expectedPri int64
	}{
		{
			name:        "1 TIA fee large gas",
			fee:         math.NewInt(1_000_000),
			gas:         1000000,
			expectedPri: 1000000,
		},
		{
			name:        "1 utia fee small gas",
			fee:         math.NewInt(1),
			gas:         1,
			expectedPri: 1000000,
		},
		{
			name:        "2 utia fee small gas",
			fee:         math.NewInt(2),
			gas:         1,
			expectedPri: 2000000,
		},
		{
			name:        "1_000_000 TIA fee normal gas tx",
			fee:         math.NewInt(1_000_000_000_000),
			gas:         75000,
			expectedPri: 13333333333333,
		},
		{
			name:        "0.001 utia gas price",
			fee:         math.NewInt(1_000),
			gas:         1_000_000,
			expectedPri: 1000,
		},
		{
			name:        "zero fee",
			fee:         math.ZeroInt(),
			gas:         1_000_000,
			expectedPri: 0,
		},
		{
			name:        "zero gas returns zero priority instead of dividing by zero",
			fee:         math.NewInt(1_000),
			gas:         0,
			expectedPri: 0,
		},
		{
			name:        "gas limit above the max int64 is not narrowed to a negative value",
			fee:         math.NewIntFromUint64(1 << 63),
			gas:         1 << 63,
			expectedPri: 1_000_000,
		},
		{
			name:        "priority that overflows int64 returns zero priority",
			fee:         math.NewIntFromUint64(1 << 63),
			gas:         1,
			expectedPri: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedPri, getTxPriority(tc.fee, tc.gas))
		})
	}
}
