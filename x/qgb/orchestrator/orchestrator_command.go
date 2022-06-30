package orchestrator

import (
	"context"
	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

func OrchestratorCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "orchestrator <flags>",
		Aliases: []string{"orch"},
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := parseOrchestratorFlags(cmd)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			return StartOrchestrator(ctx, config)
		},
	}
	return addOrchestratorFlags(command)
}

func StartOrchestrator(ctx context.Context, config orchestratorConfig) error {
	ctx, cancel := context.WithCancel(ctx)
	logger := tmlog.NewTMLogger(os.Stdout)

	querier, err := NewQuerier(config.celesGRPC, config.tendermintRPC, logger, MakeEncodingConfig())
	if err != nil {
		panic(err)
	}

	// creates the signer
	//TODO: optionally ask for input for a password
	ring, err := keyring.New("orchestrator", config.keyringBackend, config.keyringPath, strings.NewReader(""))
	if err != nil {
		panic(err)
	}
	signer := paytypes.NewKeyringSigner(
		ring,
		config.keyringAccount,
		config.celestiaChainID,
	)

	broadcaster, err := NewBroadcaster(config.celesGRPC, signer)
	if err != nil {
		panic(err)
	}

	retrier := NewRetrier(logger, 5)
	orch := NewOrchestrator(
		logger,
		querier,
		broadcaster,
		retrier,
		signer,
		*config.privateKey,
	)

	logger.Debug("starting orchestrator")

	// Listen for and trap any OS signal to gracefully shutdown and exit
	trapSignal(logger, cancel)

	orch.Start(ctx)

	return nil
}

// trapSignal will listen for any OS signal and invoke Done on the main
// WaitGroup allowing the main process to gracefully exit.
func trapSignal(logger tmlog.Logger, cancel context.CancelFunc) {
	var sigCh = make(chan os.Signal)

	signal.Notify(sigCh, syscall.SIGTERM)
	signal.Notify(sigCh, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Info("caught signal; shutting down", "signal", sig.String())
		cancel()
	}()
}
