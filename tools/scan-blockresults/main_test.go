package main

import (
	"reflect"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	sm "github.com/cometbft/cometbft/state"
)

// TestABCIResponsesKeyMatchesStore guards the key copied from cometbft: if it changes, every
// height would be reported as missing.
func TestABCIResponsesKeyMatchesStore(t *testing.T) {
	db := dbm.NewMemDB()
	stateStore := sm.NewStore(db, sm.StoreOptions{})

	const h = 42
	if err := stateStore.SaveFinalizeBlockResponse(h, &abci.ResponseFinalizeBlock{AppHash: []byte{1}}); err != nil {
		t.Fatal(err)
	}

	for height, want := range map[int64]bool{h: true, h + 1: false} {
		v, err := db.Get(abciResponsesKey(height))
		if err != nil {
			t.Fatal(err)
		}
		if got := len(v) != 0; got != want {
			t.Errorf("height %d: present=%v, want %v", height, got, want)
		}
	}
}

func TestGapCollector(t *testing.T) {
	var c gapCollector
	for _, m := range []struct {
		h            int64
		appVersion   uint64
		blockMissing bool
	}{
		{10, 3, false}, {11, 3, false}, {12, 3, false}, // one gap
		{13, 4, false}, // app version changes
		{15, 4, false}, // not contiguous
		{16, 0, true},  // block missing
		{17, 0, true},
	} {
		c.add(m.h, m.appVersion, m.blockMissing)
	}

	want := []gap{
		{start: 10, end: 12, appVersion: 3},
		{start: 13, end: 13, appVersion: 4},
		{start: 15, end: 15, appVersion: 4},
		{start: 16, end: 17, blockMissing: true},
	}
	if !reflect.DeepEqual(c.gaps, want) {
		t.Fatalf("gaps = %+v, want %+v", c.gaps, want)
	}
}
