package utils

import (
	abci "github.com/tendermint/tendermint/abci/types"
)

type AppWithMigrations interface {
	abci.Application

	RunMigrations() []byte
}
