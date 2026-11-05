package ante

import (
	"math"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestShortShareVersions checks that a PFB with more blob sizes than share
// versions, which ValidateBasic rejects, is also rejected rather than causing
// a panic if it reaches these checks first.
func TestShortShareVersions(t *testing.T) {
	msgs := []*blobtypes.MsgPayForBlobs{
		{BlobSizes: []uint32{1}},
		{BlobSizes: []uint32{1, 2}, ShareVersions: []uint32{0}},
	}
	for _, msg := range msgs {
		require.NotPanics(t, func() { _ = msg.Gas(appconsts.GasPerBlobByte) })

		_, err := getSharesNeeded(0, msg)
		require.ErrorIs(t, err, blobtypes.ErrMismatchedNumberOfPFBComponent)
		require.ErrorIs(t, validateEnoughGas(msg, appconsts.GasPerBlobByte, math.MaxUint64), blobtypes.ErrMismatchedNumberOfPFBComponent)

		err = NewMinGasPFBDecorator().validatePFBHasEnoughGas([]sdk.Msg{msg}, appconsts.GasPerBlobByte, math.MaxUint64)
		require.ErrorIs(t, err, blobtypes.ErrMismatchedNumberOfPFBComponent)
		err = NewBlobShareDecorator(fuzzBlobParams{squareSize: 128}).validateMsgs([]sdk.Msg{msg}, 0, math.MaxInt)
		require.ErrorIs(t, err, blobtypes.ErrMismatchedNumberOfPFBComponent)
	}
}
