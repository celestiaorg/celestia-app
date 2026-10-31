package app

import (
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
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
