package fibre

import (
	"testing"
)

func TestProtocolParams_RowSize(t *testing.T) {
	tests := []struct {
		name         string
		totalLen     int // dataLen + headerLen
		originalRows int
		rowSizeMin   int
		wantRowSize  int
	}{
		{
			name:         "exact fit",
			totalLen:     64 * 8, // Exactly fits in 8 rows of 64 bytes
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  64,
		},
		{
			name:         "needs rounding up",
			totalLen:     100 + blobHeaderLen,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  64, // ceil(105/8) = 14, rounded up to 64
		},
		{
			name:         "small data",
			totalLen:     1 + blobHeaderLen,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  64, // ceil(6/8) = 1, rounded up to 64
		},
		{
			name:         "large data",
			totalLen:     10000 + blobHeaderLen,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  1280, // ceil(10005/8) = 1251, rounded up to 1280 (20*64)
		},
		{
			name:         "different row size min",
			totalLen:     1000 + blobHeaderLen,
			originalRows: 4,
			rowSizeMin:   128,
			wantRowSize:  256, // ceil(1005/4) = 252, rounded up to 256 (2*128)
		},
		{
			name:         "zero length",
			totalLen:     0,
			originalRows: 8,
			rowSizeMin:   64,
			wantRowSize:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ProtocolParams{
				Rows:       tt.originalRows,
				MinRowSize: tt.rowSizeMin,
			}

			rowSize := p.RowSize(0, tt.totalLen)
			if rowSize != tt.wantRowSize {
				t.Errorf("RowSize(0, %d) = %d, want %d", tt.totalLen, rowSize, tt.wantRowSize)
			}

			// verify row size is multiple of MinRowSize (except for zero)
			if rowSize != 0 && rowSize%tt.rowSizeMin != 0 {
				t.Errorf("RowSize(%d) = %d, not a multiple of %d", tt.totalLen, rowSize, tt.rowSizeMin)
			}
		})
	}
}
