package cmd

import (
	dbm "github.com/cometbft/cometbft-db"
	tmcfg "github.com/tendermint/tendermint/config"
)

// DBContext specifies config information for loading a new DB.
type DBContext struct {
	ID     string
	Config *tmcfg.Config
}

// DefaultDBProvider returns a database using the DBBackend and DBDir
// specified in the Config.
func DefaultDBProvider(ctx *DBContext) (dbm.DB, error) {
	dbType := dbm.BackendType(ctx.Config.DBBackend)

	return dbm.NewDB(ctx.ID, dbType, ctx.Config.DBDir())
}
