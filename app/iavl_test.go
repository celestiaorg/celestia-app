package app_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store/metrics"
	"cosmossdk.io/store/rootmulti"
	storetypes "cosmossdk.io/store/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/iavl"
	idb "github.com/cosmos/iavl/db"
	ics23 "github.com/cosmos/ics23/go"
	"github.com/stretchr/testify/require"
)

// The tests in this file pin the github.com/cosmos/iavl behavior that
// celestia-app depends on. Golden values were recorded with iavl v1.2.8. If an
// iavl upgrade changes any of them, the upgrade changes the app hash, the
// on-disk format, or proofs that light clients and IBC verify, and must not be
// merged without a coordinated network upgrade.

// iavlOps applies a fixed sequence of writes and deletes for the given version.
func iavlOps(t *testing.T, tree *iavl.MutableTree, version int) {
	t.Helper()
	for i := range 50 {
		_, err := tree.Set([]byte(fmt.Sprintf("key-%03d", i*version)), []byte(fmt.Sprintf("value-%d-%d", version, i)))
		require.NoError(t, err)
	}
	if version > 1 {
		for i := 0; i < 50; i += 7 {
			_, _, err := tree.Remove([]byte(fmt.Sprintf("key-%03d", i)))
			require.NoError(t, err)
		}
	}
}

func newIAVLTree(db dbm.DB) *iavl.MutableTree {
	return iavl.NewMutableTree(idb.NewWrapper(db), 0, false, iavl.NewNopLogger())
}

// buildIAVLTree saves three versions of iavlOps and returns the root hash of each.
func buildIAVLTree(t *testing.T, db dbm.DB) (*iavl.MutableTree, []string) {
	t.Helper()
	tree := newIAVLTree(db)
	hashes := make([]string, 0, 3)
	for v := 1; v <= 3; v++ {
		iavlOps(t, tree, v)
		hash, version, err := tree.SaveVersion()
		require.NoError(t, err)
		require.Equal(t, int64(v), version)
		hashes = append(hashes, hex.EncodeToString(hash))
	}
	return tree, hashes
}

func TestIAVLRootHashes(t *testing.T) {
	empty := newIAVLTree(dbm.NewMemDB())
	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hex.EncodeToString(empty.Hash()))

	_, hashes := buildIAVLTree(t, dbm.NewMemDB())
	require.Equal(t, []string{
		"903920013239bd295915e5ec2f26b16cae2be199e68c6283f7bbbfb6cb30f3b3",
		"d8de441d670733090fd391f7455fbf5bbeba434345664199444a05d490c007c2",
		"ee509b0e3d9592e077d38fdd0daffc0e0d308fbf2b74291ba86ef0c96ab9912b",
	}, hashes)
}

func TestIAVLRootKey(t *testing.T) {
	// TestRestartAfterInterruptedCommit and the on-disk layout depend on this encoding.
	require.Equal(t, "000000000000000500000001", hex.EncodeToString(iavl.GetRootKey(5)))
}

func TestIAVLReloadAndHistoricalVersions(t *testing.T) {
	db := dbm.NewMemDB()
	tree, hashes := buildIAVLTree(t, db)

	reloaded := newIAVLTree(db)
	latest, err := reloaded.Load()
	require.NoError(t, err)
	require.Equal(t, int64(3), latest)
	require.Equal(t, hashes[2], hex.EncodeToString(reloaded.Hash()))
	require.Equal(t, []int{1, 2, 3}, reloaded.AvailableVersions())

	v1, err := reloaded.GetImmutable(1)
	require.NoError(t, err)
	require.Equal(t, hashes[0], hex.EncodeToString(v1.Hash()))
	value, err := v1.Get([]byte("key-000"))
	require.NoError(t, err)
	require.Equal(t, []byte("value-1-0"), value)

	// key-000 is removed in version 2 and must stay removed afterwards.
	value, err = reloaded.Get([]byte("key-000"))
	require.NoError(t, err)
	require.Nil(t, value)

	value, err = reloaded.GetVersioned([]byte("key-049"), 1)
	require.NoError(t, err)
	require.Equal(t, []byte("value-1-49"), value)

	// Pruning removes old versions without changing the latest root.
	require.NoError(t, tree.DeleteVersionsTo(1))
	require.False(t, tree.VersionExists(1))
	require.True(t, tree.VersionExists(2))
	_, err = tree.GetImmutable(1)
	require.ErrorIs(t, err, iavl.ErrVersionDoesNotExist)
	require.Equal(t, hashes[2], hex.EncodeToString(tree.Hash()))

	// LoadVersionForOverwriting rolls back to an older version and allows rewriting it.
	require.NoError(t, tree.LoadVersionForOverwriting(2))
	require.Equal(t, hashes[1], hex.EncodeToString(tree.Hash()))
	iavlOps(t, tree, 3)
	hash, version, err := tree.SaveVersion()
	require.NoError(t, err)
	require.Equal(t, int64(3), version)
	require.Equal(t, hashes[2], hex.EncodeToString(hash))
}

func TestIAVLProofs(t *testing.T) {
	tree, _ := buildIAVLTree(t, dbm.NewMemDB())
	immutable, err := tree.GetImmutable(3)
	require.NoError(t, err)
	root := immutable.Hash()

	key, value := []byte("key-147"), []byte("value-3-49")
	proof, err := immutable.GetMembershipProof(key)
	require.NoError(t, err)
	require.True(t, ics23.VerifyMembership(ics23.IavlSpec, root, proof, key, value))

	missing := []byte("key-000")
	proof, err = immutable.GetNonMembershipProof(missing)
	require.NoError(t, err)
	require.True(t, ics23.VerifyNonMembership(ics23.IavlSpec, root, proof, missing))
}

func TestIAVLExportImport(t *testing.T) {
	tree, hashes := buildIAVLTree(t, dbm.NewMemDB())
	immutable, err := tree.GetImmutable(3)
	require.NoError(t, err)
	exporter, err := immutable.Export()
	require.NoError(t, err)
	defer exporter.Close()

	target := newIAVLTree(dbm.NewMemDB())
	importer, err := target.Import(3)
	require.NoError(t, err)
	defer importer.Close()
	for {
		node, err := exporter.Next()
		if errors.Is(err, iavl.ErrorExportDone) {
			break
		}
		require.NoError(t, err)
		require.NoError(t, importer.Add(node))
	}
	require.NoError(t, importer.Commit())

	latest, err := target.Load()
	require.NoError(t, err)
	require.Equal(t, int64(3), latest)
	require.Equal(t, hashes[2], hex.EncodeToString(target.Hash()))
}

// TestIAVLMultiStoreCommitHashes pins the app hash produced by the store fork
// celestia-app uses on top of iavl.
func TestIAVLMultiStoreCommitHashes(t *testing.T) {
	db := dbm.NewMemDB()
	newStore := func() (*rootmulti.Store, []*storetypes.KVStoreKey) {
		ms := rootmulti.NewStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
		keys := []*storetypes.KVStoreKey{storetypes.NewKVStoreKey("bank"), storetypes.NewKVStoreKey("staking")}
		for _, key := range keys {
			ms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
		}
		require.NoError(t, ms.LoadLatestVersion())
		return ms, keys
	}

	ms, keys := newStore()
	appHashes := make([]string, 0, 2)
	for v := 1; v <= 2; v++ {
		for i, key := range keys {
			kv := ms.GetCommitKVStore(key)
			for j := range 20 {
				kv.Set([]byte(fmt.Sprintf("%d-%d", i, j*v)), []byte(fmt.Sprintf("%d-%d-%d", v, i, j)))
			}
			if v > 1 {
				kv.Delete([]byte(fmt.Sprintf("%d-0", i)))
			}
		}
		commitID := ms.Commit()
		require.Equal(t, int64(v), commitID.Version)
		appHashes = append(appHashes, hex.EncodeToString(commitID.Hash))
	}
	require.Equal(t, []string{
		"ee1ae1d4ea0fd5e9d6af0b67ed9444bbbd06d86add5c8a6e13033c5bf8fee708",
		"aabe442be3380ad2e79c878a901f15f5673d9164e08bd59c3d55aa67fbf308b7",
	}, appHashes)

	reloaded, _ := newStore()
	require.Equal(t, appHashes[1], hex.EncodeToString(reloaded.LastCommitID().Hash))
}
