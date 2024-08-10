package utils

import abci "github.com/tendermint/tendermint/abci/types"

// ApplicationWithMigrations extends the abci.Application interface with a method to run migrations.
type ApplicationWithMigrations interface {
	abci.Application

	// RunMigrations should be invoked when the app version has increased and
	// the current app needs to migrate state from the previous app.
	RunMigrations(RequestRunMigrations) ResponseRunMigrations
}

type RequestRunMigrations struct{}

type ResponseRunMigrations struct {
	DataRoot []byte
}
