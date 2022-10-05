package orchestrator

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"sync"
)

// ConfirmStoreI is a store interface for data commitment confirms and valset confirms.
type ConfirmStoreI interface {
	AddDataCommitmentConfirm(confirm types.MsgDataCommitmentConfirm) error
	GetDataCommitmentConfirms(nonce uint64) ([]types.MsgDataCommitmentConfirm, error)
	GetDataCommitmentConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgDataCommitmentConfirm, error)
	GetDataCommitmentConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgDataCommitmentConfirm, error)
	AddValsetConfirm(confirm types.MsgValsetConfirm) error
	GetValsetConfirms(nonce uint64) ([]types.MsgValsetConfirm, error)
	GetValsetConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgValsetConfirm, error)
	GetValsetConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgValsetConfirm, error)
}

// ConfirmStore is simple in memory store for data commitment confirms and valset confirms.
// To be used with the InMemoryIndexer.
type ConfirmStore struct {
	mutex                  *sync.Mutex
	DataCommitmentConfirms map[uint64][]types.MsgDataCommitmentConfirm
	ValsetConfirms         map[uint64][]types.MsgValsetConfirm
}

var _ ConfirmStoreI = &ConfirmStore{}

func NewConfirmStore() *ConfirmStore {
	return &ConfirmStore{
		DataCommitmentConfirms: make(map[uint64][]types.MsgDataCommitmentConfirm),
		ValsetConfirms:         make(map[uint64][]types.MsgValsetConfirm),
		mutex:                  &sync.Mutex{},
	}
}

func (store ConfirmStore) AddDataCommitmentConfirm(confirm types.MsgDataCommitmentConfirm) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.DataCommitmentConfirms[confirm.Nonce] = append(store.DataCommitmentConfirms[confirm.Nonce], confirm)
	return nil
}

func (store ConfirmStore) GetDataCommitmentConfirms(nonce uint64) ([]types.MsgDataCommitmentConfirm, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	confirms, ok := store.DataCommitmentConfirms[nonce]
	if !ok {
		return nil, fmt.Errorf("not existant")
	}
	return confirms, nil
}

func (store ConfirmStore) GetDataCommitmentConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgDataCommitmentConfirm, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	confirms, ok := store.DataCommitmentConfirms[nonce]
	if !ok {
		return types.MsgDataCommitmentConfirm{}, fmt.Errorf("not existent")
	}
	for _, confirm := range confirms {
		if confirm.ValidatorAddress == orch {
			return confirm, nil
		}
	}
	return types.MsgDataCommitmentConfirm{}, fmt.Errorf("not existent")
}

func (store ConfirmStore) GetDataCommitmentConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgDataCommitmentConfirm, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	confirms, ok := store.DataCommitmentConfirms[nonce]
	if !ok {
		return types.MsgDataCommitmentConfirm{}, fmt.Errorf("not existent")
	}
	for _, confirm := range confirms {
		if confirm.EthAddress == ethAddr {
			return confirm, nil
		}
	}
	return types.MsgDataCommitmentConfirm{}, fmt.Errorf("not existent")
}

func (store ConfirmStore) AddValsetConfirm(confirm types.MsgValsetConfirm) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.ValsetConfirms[confirm.Nonce] = append(store.ValsetConfirms[confirm.Nonce], confirm)
	return nil
}

func (store ConfirmStore) GetValsetConfirms(nonce uint64) ([]types.MsgValsetConfirm, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	confirms, ok := store.ValsetConfirms[nonce]
	if !ok {
		return nil, fmt.Errorf("not existant")
	}
	return confirms, nil
}

func (store ConfirmStore) GetValsetConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgValsetConfirm, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	confirms, ok := store.ValsetConfirms[nonce]
	if !ok {
		return types.MsgValsetConfirm{}, fmt.Errorf("not existent")
	}
	for _, confirm := range confirms {
		if confirm.Orchestrator == orch {
			return confirm, nil
		}
	}
	return types.MsgValsetConfirm{}, fmt.Errorf("not existent")
}

func (store ConfirmStore) GetValsetConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgValsetConfirm, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	confirms, ok := store.ValsetConfirms[nonce]
	if !ok {
		return types.MsgValsetConfirm{}, fmt.Errorf("not existent")
	}
	for _, confirm := range confirms {
		if confirm.EthAddress == ethAddr {
			return confirm, nil
		}
	}
	return types.MsgValsetConfirm{}, fmt.Errorf("not existent")
}
