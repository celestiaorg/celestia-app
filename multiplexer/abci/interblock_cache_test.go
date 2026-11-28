package abci

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v12/multiplexer/appd"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestInterBlockCacheSyncOnlyConfig(t *testing.T) {
	valid := []struct {
		mode     any
		size     any
		syncOnly bool
	}{
		{nil, nil, false},
		{"", nil, false},
		{"off", nil, false},
		{"sync-only", nil, true},
		{" Sync-Only ", nil, true},
		{"sync-only", int64(20000), true},
		{"sync-only", "5000", true},
		{"off", "many", false}, // the size is only read in sync-only mode
	}
	for _, tc := range valid {
		v := viper.New()
		if tc.mode != nil {
			v.Set(configInterBlockCache, tc.mode)
		}
		if tc.size != nil {
			v.Set(configInterBlockCacheSize, tc.size)
		}
		syncOnly, err := interBlockCacheSyncOnly(v)
		require.NoError(t, err, "%v %v", tc.mode, tc.size)
		require.Equal(t, tc.syncOnly, syncOnly, "%v %v", tc.mode, tc.size)
	}

	for _, mode := range []any{"on", "true", true, []string{"sync-only"}} {
		v := viper.New()
		v.Set(configInterBlockCache, mode)
		_, err := interBlockCacheSyncOnly(v)
		require.ErrorContains(t, err, "invalid performance.inter-block-cache", "%v", mode)
	}
	for _, size := range []any{0, -1, int64(1) << 40, "many"} {
		v := viper.New()
		v.Set(configInterBlockCache, "sync-only")
		v.Set(configInterBlockCacheSize, size)
		_, err := interBlockCacheSyncOnly(v)
		require.ErrorContains(t, err, "invalid performance.inter-block-cache-size", "%v", size)
	}
}

// An invalid performance.inter-block-cache stops the multiplexer from starting.
func TestNewMultiplexerRejectsInvalidInterBlockCache(t *testing.T) {
	versions, err := NewVersions(Version{Appd: &appd.Appd{}, ABCIVersion: ABCIClientVersion1, AppVersion: 3})
	require.NoError(t, err)

	svrCtx := server.NewDefaultContext()
	svrCtx.Viper.Set(configInterBlockCache, "on")
	_, err = NewMultiplexer(svrCtx, serverconfig.Config{}, client.Context{}, nil, versions, "test", 10, nil)
	require.ErrorContains(t, err, "invalid performance.inter-block-cache")

	svrCtx = server.NewDefaultContext()
	svrCtx.Viper.Set(configInterBlockCache, "sync-only")
	_, err = NewMultiplexer(svrCtx, serverconfig.Config{}, client.Context{}, nil, versions, "test", 10, nil)
	require.NoError(t, err)
}
