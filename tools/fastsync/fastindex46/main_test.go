package main

import (
	"bytes"
	"fmt"
	"testing"

	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
)

// writeDB commits a few versions to three IAVL stores with the fast-node
// index off, as the appliers do, and returns the last commit id.
func writeDB(t *testing.T, dir, backend string) storetypes.CommitID {
	db, err := openDB(dir, backend)
	require.NoError(t, err)
	defer db.Close()
	rs := rootmulti.NewStore(db, log.NewNopLogger())
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
	require.Contains(t, out.String(), "2 non-empty stores need a rebuild")

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
