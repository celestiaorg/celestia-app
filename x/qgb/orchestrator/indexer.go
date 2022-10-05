package orchestrator

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
)

type IndexerI interface {
	Start() error
	Stop() error
	AddDataCommitmentConfirm(confirm types.MsgDataCommitmentConfirm) error
	AddValsetConfirm(confirm types.MsgValsetConfirm) error
	//Remove() error ?
}

var _ IndexerI = &InMemoryIndexer{}

type InMemoryIndexer struct {
	Store *ConfirmStore
}

func NewInMemoryIndexer(store *ConfirmStore) *InMemoryIndexer {
	return &InMemoryIndexer{
		Store: store,
	}
}

func (indexer InMemoryIndexer) Start() error {
	return nil
}

func (indexer InMemoryIndexer) Stop() error {
	return nil
}

func (indexer InMemoryIndexer) AddDataCommitmentConfirm(confirm types.MsgDataCommitmentConfirm) error {
	return indexer.Store.AddDataCommitmentConfirm(confirm)
}

func (indexer InMemoryIndexer) AddValsetConfirm(confirm types.MsgValsetConfirm) error {
	return indexer.Store.AddValsetConfirm(confirm)
}
