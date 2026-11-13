package keeper_test

import (
	"math/big"
	"math/bits"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

// FuzzUpdateBlobParams sends MsgUpdateBlobParams with any gas per blob byte and
// governance max square size, from the governance authority or another
// address, through the wire encoding. The keeper must accept it exactly when
// the authority matches, the gas per byte is non-zero and the square size is a
// power of two no larger than the consensus upper bound, and must leave the
// stored params untouched otherwise. For accepted params, PFB gas for a blob of
// any size up to the max tx size must not wrap uint64.
func FuzzUpdateBlobParams(f *testing.F) {
	f.Add(true, appconsts.GasPerBlobByte, uint64(appconsts.DefaultGovMaxSquareSize), uint32(1000), false)
	f.Add(true, uint32(0), uint64(64), uint32(1), false)
	f.Add(true, uint32(1), uint64(0), uint32(1), true)
	f.Add(true, uint32(1), uint64(96), uint32(1), true)
	f.Add(true, ^uint32(0), uint64(appconsts.SquareSizeUpperBound), uint32(appconsts.MaxTxSize), true)
	f.Add(true, uint32(8), uint64(appconsts.SquareSizeUpperBound)*2, uint32(1), false)
	f.Add(false, uint32(8), uint64(64), uint32(1), false)

	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	f.Fuzz(func(t *testing.T, fromAuthority bool, gasPerByte uint32, maxSquare uint64, blobSize uint32, signer bool) {
		k, _, ctx := CreateKeeper(t, appconsts.Version)
		before := k.GetParams(ctx)

		msg := &types.MsgUpdateBlobParams{Authority: "celestia1notgovernance", Params: types.Params{GasPerBlobByte: gasPerByte, GovMaxSquareSize: maxSquare}}
		if fromAuthority {
			msg.Authority = authority
		}
		wire, err := msg.Marshal()
		require.NoError(t, err)
		var decoded types.MsgUpdateBlobParams
		require.NoError(t, decoded.Unmarshal(wire))
		_, err = k.UpdateBlobParams(ctx, &decoded)

		powerOfTwo := maxSquare != 0 && bits.OnesCount64(maxSquare) == 1
		want := fromAuthority && gasPerByte != 0 && powerOfTwo && maxSquare <= uint64(appconsts.SquareSizeUpperBound)
		require.Equal(t, want, err == nil, "params %+v: %v", msg.Params, err)
		after := k.GetParams(ctx)
		if !want {
			require.Equal(t, before, after, "rejected update changed the params")
			return
		}
		require.Equal(t, msg.Params, after)

		size := blobSize % (uint32(appconsts.MaxTxSize) + 1)
		pfb := &types.MsgPayForBlobs{BlobSizes: []uint32{size}, ShareVersions: []uint32{uint32(share.ShareVersionZero)}}
		if signer {
			pfb.ShareVersions[0] = uint32(share.ShareVersionOne)
		}
		shares := share.SparseSharesNeeded(size, signer)
		exact := new(big.Int).Mul(big.NewInt(int64(shares)), big.NewInt(share.ShareSize))
		exact.Mul(exact, new(big.Int).SetUint64(uint64(after.GasPerBlobByte)))
		require.True(t, exact.IsUint64(), "blob gas %s overflows uint64", exact)
		require.Equal(t, exact.Uint64(), types.GasToConsume(pfb, after.GasPerBlobByte))
	})
}
