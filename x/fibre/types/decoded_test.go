package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCertKeyFromContext(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03}
	key := sigcache.NewKey(sigcache.PffCertificate, []byte("cert"))
	d := &types.DecodedPayForFibre{Raw: raw, CertKey: key, CertKeyed: true}

	requireNoKey := func(ctx sdk.Context, msg string) {
		t.Helper()
		got, ok := types.CertKeyFromContext(ctx)
		require.False(t, ok, msg)
		require.Equal(t, sigcache.Key{}, got)
	}

	// A context without a value, or with the zero base context, yields nothing.
	requireNoKey(sdk.Context{}, "no value attached")
	requireNoKey(sdk.Context{}.WithTxBytes(raw), "no value attached")

	// The key is returned only for the exact bytes it was derived from.
	ctx := types.WithCertKey(sdk.Context{}.WithTxBytes(raw), d)
	got, ok := types.CertKeyFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, key, got)

	got, ok = types.CertKeyFromContext(ctx.WithTxBytes(append([]byte{}, raw...)))
	require.True(t, ok, "equal bytes are the same tx")
	require.Equal(t, key, got)

	requireNoKey(ctx.WithTxBytes([]byte{0x01, 0x02}), "other bytes")

	// A later tx clears it, and a view that could not be keyed never sets it.
	requireNoKey(types.WithCertKey(ctx, nil), "cleared by a nil view")
	requireNoKey(types.WithCertKey(ctx, &types.DecodedPayForFibre{Raw: raw}), "view carries no key")
	requireNoKey(types.WithCertKey(ctx, &types.DecodedPayForFibre{CertKey: key, CertKeyed: true}), "view has no bytes")
}

func TestPromiseKeyed(t *testing.T) {
	require.False(t, (&types.DecodedPayForFibre{}).PromiseKeyed())
	require.True(t, (&types.DecodedPayForFibre{PromiseSignBytes: []byte{0x01}}).PromiseKeyed())
}
