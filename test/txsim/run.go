package txsim

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// Run is the entrypoint function for starting the txsim client. The lifecycle of the client is managed
// through the context. At least one grpc and rpc endpoint must be provided. The client relies on a
// single funded master account present in the keyring. The client allocates subaccounts for sequences
// at runtime. A seed can be provided for deterministic randomness. The pollTime dictates the frequency
// that the client should check for updates from state and that transactions have been committed.
//
// This should be used for testing purposes only.
//
// All sequences can be scaled up using the `Clone` method. This allows for a single sequence that
// repeatedly sends random PFBs to be scaled up to 1000 accounts sending PFBs.
func Run(
	ctx context.Context,
	rpcEndpoints, grpcEndpoints []string,
	keys keyring.Keyring,
	seed int64,
	pollTime time.Duration,
	sequences ...Sequence,
) error {
	r := rand.New(rand.NewSource(seed))

	txClient, err := NewTxClient(ctx, encoding.MakeConfig(app.ModuleEncodingRegisters...), pollTime, rpcEndpoints)
	if err != nil {
		return err
	}

	queryClient, err := NewQueryClient(grpcEndpoints)
	if err != nil {
		return err
	}
	defer queryClient.Close()

	// Create the account manager to handle account transactions.
	manager, err := NewAccountManager(ctx, keys, txClient, queryClient)
	if err != nil {
		return err
	}

	// Initiaize each of the sequences by allowing them to allocate accounts.
	for _, sequence := range sequences {
		sequence.Init(ctx, manager.query.Conn(), manager.AllocateAccounts, r)
	}

	// Generate the allotted accounts on chain by sending them sufficient funds
	if err := manager.GenerateAccounts(ctx); err != nil {
		return err
	}

	errCh := make(chan error, len(sequences))

	// Spin up a task group to run each of the sequences concurrently.
	for idx, sequence := range sequences {
		go func(seqID int, sequence Sequence, errCh chan<- error) {
			opNum := 0
			r := rand.New(rand.NewSource(seed))
			// each sequence loops through the next set of operations, the new messages are then
			// submitted on chain
			for {
				ops, err := sequence.Next(ctx, manager.query.Conn(), r)
				if err != nil {
					errCh <- fmt.Errorf("sequence %d: %w", seqID, err)
					return
				}

				// Submit the messages to the chain.
				if err := manager.Submit(ctx, ops); err != nil {
					errCh <- fmt.Errorf("sequence %d: %w", seqID, err)
					return
				}
				opNum++
			}
		}(idx, sequence, errCh)
	}

	var finalErr error
	for i := 0; i < len(sequences); i++ {
		err := <-errCh
		if err == nil { // should never happen
			continue
		}
		if errors.Is(err, EndOfSequence) {
			log.Info().Err(err).Msg("sequence terminated")
			continue
		}
		log.Error().Err(err).Msg("sequence failed")
		finalErr = err
	}

	return finalErr
}

func Setup(t testing.TB, customParams *tmproto.ConsensusParams) (keyring.Keyring, string, string) {
	t.Helper()
	genesis, keyring, err := testnode.DefaultGenesisState()
	require.NoError(t, err)

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.RPC.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())
	tmCfg.P2P.ListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())
	tmCfg.RPC.GRPCListenAddress = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())
	tmCfg.RPC.MaxBodyBytes = 1024 * 1024 * 8 // 8MB

	if customParams == nil {
		customParams = testnode.DefaultParams()
	}

	node, app, cctx, err := testnode.New(
		t,
		customParams,
		tmCfg,
		true,
		genesis,
		keyring,
		"testnet",
	)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(node, cctx)
	require.NoError(t, err)

	appConf := testnode.DefaultAppConfig()
	appConf.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", testnode.GetFreePort())
	appConf.API.Address = fmt.Sprintf("tcp://127.0.0.1:%d", testnode.GetFreePort())

	_, cleanupGRPC, err := testnode.StartGRPCServer(app, appConf, cctx)
	require.NoError(t, err)

	t.Cleanup(func() {
		t.Log("tearing down testnode")
		require.NoError(t, stopNode())
		require.NoError(t, cleanupGRPC())
	})

	return keyring, tmCfg.RPC.ListenAddress, appConf.GRPC.Address
}
