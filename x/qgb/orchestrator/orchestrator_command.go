package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/rpc/client/http"

	tmlog "github.com/tendermint/tendermint/libs/log"

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
	workerNumber := 5 // make it a parameter
	logger := tmlog.NewTMLogger(os.Stdout)

	noncesQueue := make(chan uint64, 50)
	workerContext := NewWorkerContext(
		logger,
		config.keyringAccount,
		config.keyringPath,
		config.keyringBackend,
		config.tendermintRPC,
		config.celesGRPC,
		config.celestiaChainID,
		*config.privateKey,
		types.BridgeId,
	)
	workers := make([]*Worker, workerNumber)
	for i := range workers {
		workers[i] = NewWorker(ctx, *workerContext, noncesQueue, 5)
	}

	waitGroup.Add(1)

	// Start each blocking worker in a go-routine where the worker consumes jobs
	// off of the export queue.
	for i, w := range workers {
		logger.Debug("starting worker...", "number", i+1)
		go w.Start()
	}

	// Listen for and trap any OS signal to gracefully shutdown and exit
	trapSignal(ctx, logger)

	if config.signOldNonces {
		go enqueueMissingEvents(ctx, noncesQueue, logger, config.celesGRPC, config.tendermintRPC)
	}

	if config.signNewNonces {
		go startNewEventsListener(ctx, noncesQueue, logger, config.tendermintRPC)
	}

	// Block main process (signal capture will call WaitGroup's Done)
	waitGroup.Wait()
	return nil
}

func startNewEventsListener(ctx context.Context, queue chan uint64, logger tmlog.Logger, tendermintRPC string) {
	trpc, err := http.New(tendermintRPC, "/websocket")
	if err != nil {
		panic(err)
	}
	err = trpc.Start()
	if err != nil {
		panic(err)
	}
	defer trpc.Stop()
	// This doesn't seem to complain when the node is down
	results, err := trpc.Subscribe(
		ctx,
		"attestation-changes",
		fmt.Sprintf("%s.%s='%s'", types.EventTypeAttestationRequest, sdk.AttributeKeyModule, types.ModuleName),
	)
	if err != nil {
		panic(err)
	}
	eventName := fmt.Sprintf("%s.%s", types.EventTypeAttestationRequest, types.AttributeKeyNonce)
	logger.Info("listening for new block events...")
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			// FIXME for  some reason, we receive each nonce twice
			nonce, err := strconv.Atoi(result.Events[eventName][0])
			if err != nil {
				panic(err)
			}
			logger.Debug("enqueueing new attestation", "nonce", nonce)
			queue <- uint64(nonce)
		}
	}
}

func enqueueMissingEvents(ctx context.Context, queue chan uint64, logger tmlog.Logger, celesGRPC, tendermintRPC string) {
	querier, err := NewQuerier(celesGRPC, tendermintRPC, logger, MakeEncodingConfig())
	if err != nil {
		panic(err)
	}
	latestNonce, err := querier.QueryLatestAttestationNonce(ctx)
	if err != nil {
		panic(err)
	}
	logger.Info("syncing missing nonces...", "latest_nonce", latestNonce)
	// +2 to be sure we're not missing any nonce due to some delay when starting the pool
	for i := uint64(1); i <= latestNonce+2; i++ {
		logger.Debug("enqueueing missing nonce", "nonce", i)
		// TODO enqueue only if not already signed
		queue <- i
	}
}

// trapSignal will listen for any OS signal and invoke Done on the main
// WaitGroup allowing the main process to gracefully exit.
func trapSignal(ctx context.Context, logger tmlog.Logger) {
	var sigCh = make(chan os.Signal)

	signal.Notify(sigCh, syscall.SIGTERM)
	signal.Notify(sigCh, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Info("caught signal; shutting down...", "signal", sig.String())
		defer waitGroup.Done()
	}()
}
