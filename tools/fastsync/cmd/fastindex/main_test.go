package main

import (
	"bytes"
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	pruningtypes "cosmossdk.io/store/pruning/types"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"
)

// writeDB commits a few versions to three IAVL stores with the fast-node
// index off, as the appliers do, and returns the last commit id.
func writeDB(t *testing.T, dir, backend string) storetypes.CommitID {
	db, err := dbm.NewDB("application", dbm.BackendType(backend), dir)
	require.NoError(t, err)
	defer db.Close()
	rs := rootmulti.NewStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	rs.SetPruning(pruningtypes.NewPruningOptions(pruningtypes.PruningNothing))
	rs.SetIAVLDisableFastNode(true)
	keys := []*storetypes.KVStoreKey{storetypes.NewKVStoreKey("acc"), storetypes.NewKVStoreKey("bank"), storetypes.NewKVStoreKey("empty")}
	for _, k := range keys {
		rs.MountStoreWithDB(k, storetypes.StoreTypeIAVL, nil)
	}
	require.NoError(t, rs.LoadLatestVersion())
	var id storetypes.CommitID
	for v := 1; v <= 5; v++ {
		for _, k := range keys[:2] {
			s := rs.GetKVStore(k)
			for i := range 20 {
				s.Set(fmt.Appendf(nil, "key-%d-%d", v, i), fmt.Appendf(nil, "value-%d", v*i))
			}
			s.Delete(fmt.Appendf(nil, "key-%d-0", v-1))
		}
		id = rs.Commit()
	}
	return id
}

func TestFastIndex(t *testing.T) {
	for _, backend := range []string{"goleveldb", "pebbledb"} {
		t.Run(backend, func(t *testing.T) { testFastIndex(t, backend) })
	}
}

func testFastIndex(t *testing.T, backend string) {
	dir := t.TempDir()
	id := writeDB(t, dir, backend)

	var out bytes.Buffer
	require.ErrorIs(t, run([]string{"-db", dir, "-backend", backend, "-check"}, &out), errNeedsRebuild)
	require.Contains(t, out.String(), "3 non-empty stores need a rebuild")

	out.Reset()
	require.NoError(t, run([]string{"-db", dir, "-backend", backend}, &out), out.String())
	require.Contains(t, out.String(), fmt.Sprintf("commit id unchanged (5/%X)", id.Hash))
	require.Contains(t, out.String(), "after: version 5, 3 IAVL stores, 0 non-empty stores need a rebuild")

	out.Reset()
	require.NoError(t, run([]string{"-db", dir, "-backend", backend, "-check"}, &out), out.String())
}

func TestFastIndexErrors(t *testing.T) {
	require.ErrorContains(t, run(nil, &bytes.Buffer{}), "-db is required")
	require.Error(t, run([]string{"-db", t.TempDir()}, &bytes.Buffer{}))
}
