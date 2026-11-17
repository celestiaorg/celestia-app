package appconsts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCortoV12Policy(t *testing.T) {
	for _, chain := range []string{CortoChainID, MochaChainID, MainnetChainID, TestChainID, "corto-2"} {
		for _, version := range []uint64{11, 12, 13} {
			active := chain == CortoChainID && version >= 12
			require.Equal(t, active, CortoV12Active(chain, version))
			duration, blocks := GetEvidenceAgeLimits(chain, version)
			if active {
				require.Equal(t, 10*time.Minute, GetUnbondingTime(chain, version))
				require.Equal(t, 10*time.Minute, duration)
				require.Equal(t, int64(500), blocks)
				require.Equal(t, duration, time.Duration(blocks)*maxAssumedBlockTime)
				require.Equal(t, int64(4_500), GetUpgradeHeightDelay(chain, version))
				require.Equal(t, time.Hour, time.Duration(GetUpgradeHeightDelay(chain, version))*GetConsensusTimeouts(version).BlockInterval())
			} else {
				require.Equal(t, UnbondingTime, GetUnbondingTime(chain, version))
				require.Equal(t, MaxAgeDuration, duration)
				require.Equal(t, int64(MaxAgeNumBlocks), blocks)
				require.Equal(t, GetUpgradeHeightDelay(chain, 11), GetUpgradeHeightDelay(chain, version))
			}
		}
	}
}
