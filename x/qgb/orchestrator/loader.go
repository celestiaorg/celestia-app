package orchestrator

import "github.com/celestiaorg/celestia-app/x/qgb/types"

type LoaderI interface {
	Start() error
	Stop() error
	GetDataCommitmentConfirms(nonce uint64) ([]types.MsgDataCommitmentConfirm, error)
	GetDataCommitmentConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgDataCommitmentConfirm, error)
	GetDataCommitmentConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgDataCommitmentConfirm, error)
	GetValsetConfirms(nonce uint64) ([]types.MsgValsetConfirm, error)
	GetValsetConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgValsetConfirm, error)
	GetValsetConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgValsetConfirm, error)
}

type InMemoryLoader struct {
	store ConfirmStore
}

var _ LoaderI = &InMemoryLoader{}

func NewInMemoryLoader(store ConfirmStore) *InMemoryLoader {
	return &InMemoryLoader{store: store}
}

func (loader InMemoryLoader) Start() error {
	return nil
}

func (loader InMemoryLoader) Stop() error {
	return nil
}

func (loader InMemoryLoader) GetDataCommitmentConfirms(nonce uint64) ([]types.MsgDataCommitmentConfirm, error) {
	return loader.store.GetDataCommitmentConfirms(nonce)
}

func (loader InMemoryLoader) GetDataCommitmentConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgDataCommitmentConfirm, error) {
	return loader.store.GetDataCommitmentConfirmByOrchestratorAddress(nonce, orch)
}

func (loader InMemoryLoader) GetDataCommitmentConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgDataCommitmentConfirm, error) {
	return loader.store.GetDataCommitmentConfirmByEthereumAddress(nonce, ethAddr)
}

func (loader InMemoryLoader) GetValsetConfirms(nonce uint64) ([]types.MsgValsetConfirm, error) {
	return loader.store.GetValsetConfirms(nonce)
}

func (loader InMemoryLoader) GetValsetConfirmByEthereumAddress(nonce uint64, ethAddr string) (types.MsgValsetConfirm, error) {
	return loader.store.GetValsetConfirmByEthereumAddress(nonce, ethAddr)
}

func (loader InMemoryLoader) GetValsetConfirmByOrchestratorAddress(nonce uint64, orch string) (types.MsgValsetConfirm, error) {
	return loader.store.GetValsetConfirmByOrchestratorAddress(nonce, orch)
}
