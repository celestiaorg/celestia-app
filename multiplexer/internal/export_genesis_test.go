package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/pkg/hardfork/genesis"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

// A genesis from "genesis from-export" is classified by chain-id only; the tool refuses mocha-4.
func TestGetGenesisVersionFromExport(t *testing.T) {
	params := cmttypes.DefaultConsensusParams()
	params.Version.App = appconsts.Version
	for _, tc := range []struct {
		chainID string
		want    GenesisVersion
	}{{"mocha-5", GenesisVersion2}, {"test", GenesisVersion2}, {"mocha-4", GenesisVersion1}} {
		g := &genesis.Genesis{
			GenesisTime:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			ChainID:       tc.chainID,
			InitialHeight: 12,
			Modules:       []genesis.Module{{Name: "bank", Raw: []byte(`{}`)}},
			Consensus:     &genutiltypes.ConsensusGenesis{Params: params},
		}
		path := filepath.Join(t.TempDir(), "genesis.json")
		f, err := os.Create(path)
		require.NoError(t, err)
		_, err = genesis.WriteGenesis(f, g)
		require.NoError(t, err)
		require.NoError(t, f.Close())

		got, err := GetGenesisVersion(path)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, tc.chainID)
	}
}
