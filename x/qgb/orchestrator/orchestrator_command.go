package orchestrator

import (
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/types/errors"
	corerpctypes "github.com/tendermint/tendermint/rpc/core/types"
	coretypes "github.com/tendermint/tendermint/types"
	"os"
	"os/signal"
	"strconv"
	"sync"
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
	logger := tmlog.NewTMLogger(os.Stdout)

	noncesQueue := make(chan uint64, 100)
	orch := NewOrchestrator(
		ctx,
		logger,
		config.keyringAccount,
		config.keyringPath,
		config.keyringBackend,
		config.tendermintRPC,
		config.celesGRPC,
		config.celestiaChainID,
		*config.privateKey,
		types.BridgeId,
		noncesQueue,
		5,
	)

	logger.Debug("starting orchestrator")

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go orch.Start()

	// Listen for and trap any OS signal to gracefully shutdown and exit
	orch.trapSignal(wg)

	if config.signOldNonces {
		go enqueueMissingEvents(ctx, noncesQueue, logger, config.celesGRPC, config.tendermintRPC)
	}

	if config.signNewNonces {
		go startNewEventsListener(ctx, noncesQueue, logger, config.tendermintRPC)
	}

	// Block main process (signal capture will call WaitGroup's Done)
	wg.Wait()
	return nil
}

func startNewEventsListener(ctx context.Context, queue chan<- uint64, logger tmlog.Logger, tendermintRPC string) {
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
	attestationEventName := fmt.Sprintf("%s.%s", types.EventTypeAttestationRequest, types.AttributeKeyNonce)
	logger.Info("listening for new block events...")
	for {
		select {
		case <-ctx.Done():
			return
		case result := <-results:
			blockEvent := mustGetEvent(result, coretypes.EventTypeKey)
			isBlock := blockEvent[0] == coretypes.EventNewBlock
			if !isBlock {
				// we only want to handle the attestation when the block is committed
				continue
			}
			attestationEvent := mustGetEvent(result, attestationEventName)
			nonce, err := strconv.Atoi(attestationEvent[0])
			if err != nil {
				panic(err)
			}
			logger.Debug("enqueueing new attestation nonce", "nonce", nonce)
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
	lastUnbondingHeight, err := querier.QueryLastUnbondingHeight(ctx)
	if err != nil {
		panic(err)
	}
	logger.Info("syncing missing nonces", "latest_nonce", latestNonce, "last_unbonding_height", lastUnbondingHeight)
	defer logger.Info("finished syncing missing nonces", "latest_nonce", latestNonce, "last_unbonding_height", lastUnbondingHeight)
	// TODO make sure the latestNonce+1 was enqueied in the new events listener
	for i := lastUnbondingHeight; i < latestNonce; i++ {
		logger.Debug("enqueueing missing attestation nonce", "nonce", latestNonce-i)
		queue <- latestNonce - i
	}
}

// trapSignal will listen for any OS signal and invoke Done on the main
// WaitGroup allowing the main process to gracefully exit.
func (orch Orchestrator) trapSignal(wg *sync.WaitGroup) {
	var sigCh = make(chan os.Signal)

	signal.Notify(sigCh, syscall.SIGTERM)
	signal.Notify(sigCh, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		orch.logger.Info("caught signal; shutting down", "signal", sig.String())
		orch.Stop()
		defer wg.Done()
	}()
}

// mustGetEvent takes a corerpctypes.ResultEvent and checks whether it has
// the provided eventName. If not, it panics.
func mustGetEvent(result corerpctypes.ResultEvent, eventName string) []string {
	ev := result.Events[eventName]
	if ev == nil || len(ev) == 0 {
		panic(errors.Wrap(
			types.ErrEmpty,
			fmt.Sprintf(
				"%s not found in event %s",
				coretypes.EventTypeKey,
				result.Events,
			),
		))
	}
	return ev
}
