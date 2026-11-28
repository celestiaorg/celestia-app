package hdrchain

import (
	"errors"
	"fmt"

	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
)

// OpenStores opens dir/blockstore.db and, with withState, dir/state.db of a
// stopped node as a source. Close the returned source when done.
func OpenStores(dir, backend string, withState bool) (*BlockstoreSource, error) {
	bdb, err := dbm.NewDB("blockstore", dbm.BackendType(backend), dir)
	if err != nil {
		return nil, fmt.Errorf("open blockstore in %s (is the node stopped?): %w", dir, err)
	}
	s := &BlockstoreSource{Blocks: store.NewBlockStore(bdb), dbs: []dbm.DB{bdb}}
	if withState {
		sdb, err := dbm.NewDB("state", dbm.BackendType(backend), dir)
		if err != nil {
			_ = bdb.Close()
			return nil, fmt.Errorf("open state in %s (is the node stopped?): %w", dir, err)
		}
		s.State = sm.NewStore(sdb, sm.StoreOptions{DiscardABCIResponses: true})
		s.dbs = append(s.dbs, sdb)
	}
	return s, nil
}

// Close closes the databases OpenStores opened.
func (s *BlockstoreSource) Close() error {
	var err error
	for _, db := range s.dbs {
		err = errors.Join(err, db.Close())
	}
	return err
}
