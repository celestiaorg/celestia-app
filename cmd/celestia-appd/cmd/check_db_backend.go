package cmd

import (
	"fmt"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// checkDBBackend checks if the node is configured to use PebbleDB and blocks
// startup if not. PebbleDB is the only supported database backend. Node
// operators using other backends should migrate to PebbleDB using the
// migrate-db tool.
func checkDBBackend(cmd *cobra.Command, _ log.Logger) error {
	serverCtx := server.GetServerContextFromCmd(cmd)
	dbBackend := server.GetAppDBBackend(serverCtx.Viper)
	if dbBackend != dbm.PebbleDBBackend {
		return fmt.Errorf("%q is not a supported database backend. "+
			"Please migrate to PebbleDB using the migrate-db tool: "+
			"https://github.com/celestiaorg/celestia-app/tree/main/tools/migrate-db", dbBackend)
	}
	return nil
}
