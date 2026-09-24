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

func TestDefaultBlobConfig_UploadSize(t *testing.T) {
	cfg := DefaultBlobConfigV0()
	const step = 32 << 20
	for _, tt := range []struct {
		name    string
		dataLen int
		want    int
	}{
		{"single byte", 1, step},
		{"exact first step including header", step - blobHeaderLen, step},
		{"header crosses first step", step - blobHeaderLen + 1, 2 * step},
		{"32 MiB payload plus header", step, 2 * step},
		{"maximum payload", cfg.MaxDataSize, 128 << 20},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.UploadSize(tt.dataLen); got != tt.want {
				t.Errorf("UploadSize(%d) = %d, want %d", tt.dataLen, got, tt.want)
			}
		})
	}
}
