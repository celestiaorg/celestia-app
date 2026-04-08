package cmd

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/viper"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
)

// TestStartStandAlone verifies that startStandAlone does not return an error
// during startup. This is a regression test for a bug where startTelemetry was
// called twice in startStandAlone. The second call attempted to re-register the
// Prometheus metrics collector with the global prometheus.DefaultRegisterer,
// which returned a "duplicate metrics collector registration attempted" error.
// Telemetry must be enabled in the test config (with PrometheusRetentionTime > 0)
// so that startTelemetry exercises the Prometheus registration path rather than
// short-circuiting.
func TestStartStandAlone(t *testing.T) {
	homeDir := t.TempDir()

	tmCfg := tmconfig.DefaultConfig()
	tmCfg.SetRoot(homeDir)

	appCfg := serverconfig.DefaultConfig()
	appCfg.Telemetry.Enabled = true
	appCfg.Telemetry.ServiceName = "test"
	appCfg.Telemetry.PrometheusRetentionTime = 60
	appCfg.GRPC.Enable = true
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", mustGetFreePort())
	appCfg.API.Enable = true
	appCfg.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", mustGetFreePort())

	gen := genesis.NewDefaultGenesis().
		WithValidators(genesis.NewDefaultValidator("validator"))
	if err := genesis.InitFiles(homeDir, tmCfg, appCfg, gen, 0); err != nil {
		t.Fatal(err)
	}

	v := viper.New()
	v.Set("home", homeDir)
	v.SetConfigFile(fmt.Sprintf("%s/config/app.toml", homeDir))
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	serverCtx := server.NewContext(v, tmCfg, log.NewNopLogger())

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	clientCtx := client.Context{}.
		WithCodec(encCfg.Codec).
		WithInterfaceRegistry(encCfg.InterfaceRegistry).
		WithTxConfig(encCfg.TxConfig).
		WithLegacyAmino(encCfg.Amino).
		WithHomeDir(homeDir)

	errCh := make(chan error, 1)
	go func() {
		errCh <- startStandAlone(serverCtx, clientCtx, testnode.DefaultAppCreator())
	}()

	select {
	case err := <-errCh:
		t.Fatalf("startStandAlone returned an unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		// startStandAlone is blocking on WaitForQuitSignals, which means
		// it started successfully without a duplicate telemetry error.
	}
}

func mustGetFreePort() int {
	port, err := testnode.GetFreePort()
	if err != nil {
		panic(err)
	}
	return port
}
