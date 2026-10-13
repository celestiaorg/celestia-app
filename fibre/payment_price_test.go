package fibre

import (
	"fmt"
	"testing"

	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

func TestPaymentPrice(t *testing.T) {
	const mib = 1 << 20
	vanillaGas := blobtypes.DefaultEstimateGas(&blobtypes.MsgPayForBlobs{
		BlobSizes: []uint32{mib}, ShareVersions: []uint32{0},
	})
	// Round each vanilla transaction's fee up to whole utia at 0.004 utia/gas.
	vanillaFee := (vanillaGas*4 + 999) / 1000
	for _, tt := range []struct {
		mib  int
		want int64
	}{
		{1, 3_500},
		{10, 9_980},
		{100, 74_780},
	} {
		t.Run(fmt.Sprintf("%d MiB", tt.mib), func(t *testing.T) {
			uploadSize := DefaultBlobConfigV0().UploadSize(tt.mib * mib)
			payment := fibretypes.PaymentAmount(uint32(uploadSize)).Amount.Int64()
			require.Equal(t, tt.want, payment)
			require.Less(t, payment, int64(vanillaFee)*int64(tt.mib))
		})
	}
}
