// Command malicious-appd is celestia-appd with the byzantine proposer app from
// test/util/malicious. It exists only for the docker e2e tests and must never
// be released. The release builds (Makefile build targets, goreleaser and the
// release Dockerfiles) build ./cmd/celestia-appd and never this package.
//
// The start command reads the misbehaviour from the environment:
//
//	MALICIOUS_BEHAVIOR      a handler name from malicious.App.PrepareProposalHandlerMap
//	MALICIOUS_START_HEIGHT  the first height at which the node misbehaves
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/appdb"
	"github.com/celestiaorg/celestia-app/v11/cmd/celestia-appd/cmd"
	"github.com/celestiaorg/celestia-app/v11/test/util/malicious"
	"github.com/cometbft/cometbft/proxy"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/server"
	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cobra"
)

const (
	behaviorEnvVar    = "MALICIOUS_BEHAVIOR"
	startHeightEnvVar = "MALICIOUS_START_HEIGHT"
)

func main() {
	rootCmd := cmd.NewRootCmd()
	if err := useMaliciousApp(rootCmd); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := svrcmd.Execute(rootCmd, app.EnvPrefix, app.NodeHome); err != nil {
		os.Exit(1)
	}
}

// useMaliciousApp makes the start command run the malicious app. It keeps the
// flags and pre-start hooks of the celestia-appd start command.
func useMaliciousApp(rootCmd *cobra.Command) error {
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		return fmt.Errorf("finding start command: %w", err)
	}
	startCmd.RunE = func(c *cobra.Command, args []string) error {
		behavior, err := behaviorFromEnv()
		if err != nil {
			return err
		}
		maliciousStart := server.StartCmdWithOptions(newAppCreator(behavior), app.NodeHome, server.StartCmdOptions{
			DBOpener:      appdb.Open,
			PreNodeStart:  app.WirePendingSequencer,
			ClientCreator: proxy.NewConnSyncLocalClientCreator,
		})
		return maliciousStart.RunE(c, args)
	}
	return nil
}

func behaviorFromEnv() (malicious.BehaviorConfig, error) {
	name := os.Getenv(behaviorEnvVar)
	if _, ok := (&malicious.App{}).PrepareProposalHandlerMap()[name]; !ok {
		return malicious.BehaviorConfig{}, fmt.Errorf("%s=%q is not a known malicious behavior", behaviorEnvVar, name)
	}
	startHeight, err := strconv.ParseInt(os.Getenv(startHeightEnvVar), 10, 64)
	if err != nil || startHeight < 1 {
		return malicious.BehaviorConfig{}, fmt.Errorf("%s must be a positive height: %v", startHeightEnvVar, err)
	}
	return malicious.BehaviorConfig{HandlerName: name, StartHeight: startHeight}, nil
}

// newAppCreator builds the honest app exactly as celestia-appd does and wraps
// it in the malicious app.
func newAppCreator(behavior malicious.BehaviorConfig) servertypes.AppCreator {
	return func(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
		honest, ok := cmd.NewAppServer(logger, db, traceStore, appOpts).(*app.App)
		if !ok {
			panic("celestia-appd app server is not *app.App")
		}
		badApp := &malicious.App{App: honest}
		badApp.SetMaliciousBehavior(behavior)
		logger.Info("running malicious app", "behavior", behavior.HandlerName, "start_height", behavior.StartHeight)
		return badApp
	}
}
