//go:build !rocksdb
// +build !rocksdb

package cmd

import (
	"github.com/spf13/cobra"
)

func ChangeSetCmd() *cobra.Command {
	return nil
}
