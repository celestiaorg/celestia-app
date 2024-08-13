package utils

import (
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

type AppWithMigrations interface {
	abci.Application

	RunMigrations() []byte
	GetCommitMultiStore() storetypes.CommitMultiStore
	SetCommitMultiStore(cms storetypes.CommitMultiStore)
}
