package cmd

import (
	"context"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestValidateAPIConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		args    []string
		wantErr bool
	}{
		{name: "both disabled", config: "[api]\nenable = false\n[grpc]\nenable = false"},
		{name: "grpc only", config: "[api]\nenable = false\n[grpc]\nenable = true"},
		{name: "both enabled", config: "[api]\nenable = true\n[grpc]\nenable = true"},
		{name: "rest without grpc", config: "[api]\nenable = true\n[grpc]\nenable = false", wantErr: true},
		{name: "flag enables grpc", config: "[api]\nenable = true\n[grpc]\nenable = false", args: []string{"--grpc.enable=true"}},
		{name: "flag disables grpc", config: "[api]\nenable = true\n[grpc]\nenable = true", args: []string{"--grpc.enable=false"}, wantErr: true},
		{name: "flag enables rest", config: "[api]\nenable = false\n[grpc]\nenable = false", args: []string{"--api.enable=true"}, wantErr: true},
		{name: "flag disables rest", config: "[api]\nenable = true\n[grpc]\nenable = false", args: []string{"--api.enable=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "start"}
			cmd.Flags().Bool(server.FlagAPIEnable, false, "")
			cmd.Flags().Bool("grpc.enable", true, "")
			require.NoError(t, cmd.ParseFlags(tc.args))

			sctx := server.NewDefaultContext()
			sctx.Viper.SetConfigType("toml")
			require.NoError(t, sctx.Viper.ReadConfig(strings.NewReader(tc.config)))
			require.NoError(t, sctx.Viper.BindPFlags(cmd.Flags()))
			cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))

			err := validateAPIConfig(cmd, log.NewNopLogger())
			if tc.wantErr {
				require.EqualError(t, err, "REST API requires Cosmos SDK gRPC; enable grpc.enable in app.toml or pass --grpc.enable=true")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
