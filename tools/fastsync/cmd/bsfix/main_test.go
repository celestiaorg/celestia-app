package main

import (
	"bytes"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

func blockstoreHeight(t *testing.T, dir string) (base, height int64) {
	t.Helper()
	db, err := dbm.NewDB("blockstore", dbm.GoLevelDBBackend, dir)
	require.NoError(t, err)
	bs := store.NewBlockStore(db)
	defer bs.Close()
	return bs.Base(), bs.Height()
}

func TestBsfix(t *testing.T) {
	c := testchain.New(t, 8)
	for _, tc := range []struct {
		name          string
		blocks, state int64
		dry           bool
		wantHeight    int64
		wantOut       string
	}{
		{"two blocks ahead", 7, 4, false, 5, "deleted block 6 -> blockstore height 5"},
		{"one block ahead is fine", 5, 4, false, 5, "nothing to do"},
		{"in sync", 4, 4, false, 4, "nothing to do"},
		{"dry run", 7, 4, true, 7, "would delete blocks 6..7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			c.WriteStores(t, dir, tc.blocks, tc.state)
			var out bytes.Buffer
			require.NoError(t, run([]string{"-data", dir, "-backend", "goleveldb", boolFlag(tc.dry)}, &out))
			require.Contains(t, out.String(), tc.wantOut)
			base, height := blockstoreHeight(t, dir)
			require.Equal(t, int64(1), base)
			require.Equal(t, tc.wantHeight, height)

			// the state store is untouched
			sdb, err := dbm.NewDB("state", dbm.GoLevelDBBackend, dir)
			require.NoError(t, err)
			st, err := sm.NewStore(sdb, sm.StoreOptions{}).Load()
			require.NoError(t, err)
			require.Equal(t, tc.state, st.LastBlockHeight)
			require.NoError(t, sdb.Close())
		})
	}
}

func TestBsfixErrors(t *testing.T) {
	require.ErrorContains(t, run(nil, &bytes.Buffer{}), "-data is required")
	require.ErrorContains(t, run([]string{"-data", t.TempDir()}, &bytes.Buffer{}), "blockstore.db")
}

func boolFlag(dry bool) string {
	if dry {
		return "-dry-run"
	}
	return "-dry-run=false"
}
