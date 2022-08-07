package orchestrator

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/spf13/cobra"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

func OrchCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "orchestrator <flags>",
		Aliases: []string{"orch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseOrchestratorFlags(cmd)
			if err != nil {
				return err
			}
			logger := tmlog.NewTMLogger(os.Stdout)

			logger.Debug("initializing orchestrator")

			ctx, cancel := context.WithCancel(cmd.Context())

			encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

			querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger, encCfg)
			if err != nil {
				panic(err)
			}

			// creates the Signer
			// TODO: optionally ask for input for a password
			ring, err := keyring.New("orchestrator", config.keyringBackend, config.keyringPath, strings.NewReader(""), encCfg.Codec)
			if err != nil {
				panic(err)
			}
			signer := paytypes.NewKeyringSigner(
				ring,
				config.keyringAccount,
				config.celestiaChainID,
			)

			broadcaster, err := NewBroadcaster(config.celesGRPC, signer, config.celestiaGasLimit)
			if err != nil {
				panic(err)
			}

			retrier := NewRetrier(logger, 5)
			orch, err := NewOrchestrator(
				logger,
				querier,
				broadcaster,
				retrier,
				signer,
				*config.privateKey,
			)
			if err != nil {
				panic(err)
			}

			logger.Debug("starting orchestrator")

			// Listen for and trap any OS signal to gracefully shutdown and exit
			go trapSignal(logger, cancel)

			orch.Start(ctx)

			return nil
		},
	}
	return addOrchestratorFlags(command)
}

// trapSignal will listen for any OS signal and gracefully exit.
func trapSignal(logger tmlog.Logger, cancel context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, syscall.SIGTERM)
	signal.Notify(sigCh, syscall.SIGINT)

	sig := <-sigCh
	logger.Info("caught signal; shutting down...", "signal", sig.String())
	cancel()
}
