package cmd

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDBBackend(t *testing.T) {
	testCases := []struct {
		name      string
		dbBackend string
		wantErr   bool
	}{
		{
			name:      "pebbledb is allowed",
			dbBackend: "pebbledb",
			wantErr:   false,
		},
		{
			name:      "goleveldb is rejected",
			dbBackend: "goleveldb",
			wantErr:   true,
		},
		{
			name:      "default (empty) is rejected because GetAppDBBackend defaults to goleveldb",
			dbBackend: "",
			wantErr:   true,
		},
		{
			name:      "rocksdb is rejected",
			dbBackend: "rocksdb",
			wantErr:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}

			v := viper.New()
			v.Set("app-db-backend", tc.dbBackend)

			sctx := server.NewDefaultContext()
			sctx.Viper = v

			ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
			cmd.SetContext(ctx)

			err := checkDBBackend(cmd, log.NewNopLogger())
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not a supported database backend")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
