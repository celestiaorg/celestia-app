package abci

import (
	"fmt"
	"math"
	"strings"

	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

// The embedded v4-v8 apps read these app.toml keys themselves. In "sync-only"
// mode they turn on the inter-block cache that the multiplexer otherwise keeps
// off for them (--inter-block-cache=false, see #7750), and refuse every
// transaction, query and state sync request while they run, so nothing reads
// the cache concurrently with block execution. The multiplexer only validates
// the keys at startup, so that a typo fails here instead of when the first
// embedded v4 app starts, possibly hours into a sync.
const (
	configInterBlockCache     = "performance.inter-block-cache"
	configInterBlockCacheSize = "performance.inter-block-cache-size"

	interBlockCacheModeOff      = "off"
	interBlockCacheModeSyncOnly = "sync-only"
)

// interBlockCacheSyncOnly validates performance.inter-block-cache and
// performance.inter-block-cache-size and reports whether the mode is
// "sync-only". A missing or empty mode means "off".
func interBlockCacheSyncOnly(v *viper.Viper) (bool, error) {
	raw := v.Get(configInterBlockCache)
	mode, err := cast.ToStringE(raw)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if err != nil || (mode != "" && mode != interBlockCacheModeOff && mode != interBlockCacheModeSyncOnly) {
		return false, fmt.Errorf("invalid %s %q: must be %q or %q", configInterBlockCache, fmt.Sprint(raw), interBlockCacheModeOff, interBlockCacheModeSyncOnly)
	}
	if rawSize := v.Get(configInterBlockCacheSize); rawSize != nil {
		size, err := cast.ToInt64E(rawSize)
		if err != nil || size <= 0 || size > math.MaxInt32 {
			return false, fmt.Errorf("invalid %s %q: must be an integer between 1 and %d", configInterBlockCacheSize, fmt.Sprint(rawSize), math.MaxInt32)
		}
	}
	return mode == interBlockCacheModeSyncOnly, nil
}
