package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestDecodedPayForFibreFromContext(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03}
	d := &types.DecodedPayForFibre{Raw: raw}

	// A context without a value, or with the zero base context, yields nil.
	require.Nil(t, types.DecodedPayForFibreFromContext(sdk.Context{}))
	require.Nil(t, types.DecodedPayForFibreFromContext(sdk.Context{}.WithTxBytes(raw)))

	// The value is returned only for the exact bytes it was decoded from.
	ctx := types.WithDecodedPayForFibre(sdk.Context{}.WithTxBytes(raw), d)
	require.Same(t, d, types.DecodedPayForFibreFromContext(ctx))
	require.Same(t, d, types.DecodedPayForFibreFromContext(ctx.WithTxBytes(append([]byte{}, raw...))))
	require.Nil(t, types.DecodedPayForFibreFromContext(ctx.WithTxBytes([]byte{0x01, 0x02})))

	// A later tx clears it with nil.
	require.Nil(t, types.DecodedPayForFibreFromContext(types.WithDecodedPayForFibre(ctx, nil)))
}
