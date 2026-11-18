package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	teeismtypes "github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/stretchr/testify/require"
)

// TestV11StoreLoader checks that the v11 upgrade adds the teeism store to a
// database written before v11 and loads a database that already has it.
func TestV11StoreLoader(t *testing.T) {
	// newStore mounts every app store, optionally skipping teeism.
	newStore := func(db dbm.DB, withTeeism bool) storetypes.CommitMultiStore {
		ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
		for _, name := range allStoreKeys() {
			if name == teeismtypes.StoreKey && !withTeeism {
				continue
			}
			ms.MountStoreWithDB(storetypes.NewKVStoreKey(name), storetypes.StoreTypeIAVL, nil)
		}
		return ms
	}
	// commitOnce writes version 1 of a database with or without teeism.
	commitOnce := func(t *testing.T, withTeeism bool) dbm.DB {
		db := dbm.NewMemDB()
		ms := newStore(db, withTeeism)
		require.NoError(t, ms.LoadLatestVersion())
		ms.Commit()
		return db
	}
	const upgradeHeight = 2

	t.Run("adds the store to a pre-v11 database", func(t *testing.T) {
		db := commitOnce(t, false)
		require.ErrorContains(t, baseapp.DefaultStoreLoader(newStore(db, true)), "new stores should be added using StoreUpgrades")

		ms := newStore(db, true)
		require.NoError(t, v11StoreLoader(upgradeHeight)(ms))
		ms.Commit()
		require.True(t, hasStore(ms, upgradeHeight, teeismtypes.StoreKey))
	})

	t.Run("loads a database that already has the store", func(t *testing.T) {
		db := commitOnce(t, true)
		ms := newStore(db, true)
		require.NoError(t, v11StoreLoader(upgradeHeight)(ms))
		require.True(t, hasStore(ms, upgradeHeight-1, teeismtypes.StoreKey))
	})
}

// TestNativeAppOnPreTeeismState reproduces the startup the multiplexer does
// when it hands the native app a database written before the upgrade that adds
// the teeism store. The native app opens such a database only once the
// previous release has written the upgrade plan; without one it refuses to
// start. The multiplexer embeds releases up to v10 only, so today it serves app
// version 11 natively and a v11 node panics on this binary before the v12
// upgrade. Embedding the v11 release (left for a follow-up in #8634) makes the
// first subtest unreachable on a real chain and leaves the second as the only
// path.
func TestNativeAppOnPreTeeismState(t *testing.T) {
	// preTeeismDB commits version 1 of a database with every store but teeism.
	preTeeismDB := func(t *testing.T) dbm.DB {
		db := dbm.NewMemDB()
		ms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
		for _, name := range allStoreKeys() {
			if name != teeismtypes.StoreKey {
				ms.MountStoreWithDB(storetypes.NewKVStoreKey(name), storetypes.StoreTypeIAVL, nil)
			}
		}
		require.NoError(t, ms.LoadLatestVersion())
		ms.Commit()
		return db
	}
	// withHome points the upgrade keeper at an empty node home.
	withHome := func(t *testing.T) string {
		original := NodeHome
		NodeHome = t.TempDir()
		t.Cleanup(func() { NodeHome = original })
		return NodeHome
	}
	newNative := func(db dbm.DB) *App {
		return New(log.NewNopLogger(), db, nil, time.Second, 0, noAppOptions{})
	}

	t.Run("refuses to start without the upgrade plan", func(t *testing.T) {
		withHome(t)
		db := preTeeismDB(t)
		defer func() {
			r := recover()
			require.NotNil(t, r, "the native app loaded a database without the teeism store")
			require.ErrorContains(t, r.(error), "new stores should be added using StoreUpgrades")
		}()
		newNative(db)
	})

	t.Run("adds the store at the planned upgrade height", func(t *testing.T) {
		home := withHome(t)
		plan := fmt.Sprintf(`{"name":"v%d","height":2}`, appconsts.Version)
		require.NoError(t, os.MkdirAll(filepath.Join(home, "data"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(home, "data", "upgrade-info.json"), []byte(plan), 0o600))

		native := newNative(preTeeismDB(t))
		require.NotNil(t, native.CommitMultiStore().GetCommitKVStore(native.GetKey(teeismtypes.StoreKey)))
	})
}

// noAppOptions answers every option with its default.
type noAppOptions struct{}

func (noAppOptions) Get(string) any { return nil }
