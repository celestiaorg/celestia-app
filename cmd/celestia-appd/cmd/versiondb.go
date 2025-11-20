//go:build rocksdb
// +build rocksdb

package cmd

import (
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/opendb"
	"sort"

	versiondbclient "github.com/crypto-org-chain/cronos/versiondb/client"
	"github.com/spf13/cobra"
)

func ChangeSetCmd() *cobra.Command {
	keys := app.AllStoreKeys2()
	storeNames := make([]string, 0, len(keys))
	for name := range keys {
		storeNames = append(storeNames, name)
	}
	sort.Strings(storeNames)

	return versiondbclient.ChangeSetGroupCmd(versiondbclient.Options{
		DefaultStores:     storeNames,
		OpenReadOnlyDB:    opendb.OpenReadOnlyDB,
		AppRocksDBOptions: opendb.NewRocksdbOptions,
	})
}
