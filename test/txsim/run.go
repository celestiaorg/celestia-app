package txsim

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync/atomic"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const DefaultSeed = 900183116

const (
	MiB                = 1024 * 1024
	grpcMaxRecvMsgSize = 128 * MiB
	grpcMaxSendMsgSize = 128 * MiB
)

var defaultTLSConfig = &tls.Config{InsecureSkipVerify: true}

func opBytes(op Operation) int64 {
	var n int64
	for _, b := range op.Blobs {
		n += int64(len(b.Data()))
	}
	return n
}

// Run is the entrypoint function for starting the txsim client. The lifecycle of the client is managed
// through the context. A grpc endpoint must be provided. The client relies on a
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
	grpcEndpoint string,
	keys keyring.Keyring,
	encCfg encoding.Config,
	opts *Options,
	sequences ...Sequence,
) error {
	opts.Fill()
	r := rand.New(rand.NewSource(opts.seed))

	conn, err := buildGrpcConn(grpcEndpoint, defaultTLSConfig)
	if err != nil {
		return fmt.Errorf("error connecting to %s: %w", grpcEndpoint, err)
	}

	if opts.suppressLogger {
		// TODO (@cmwaters): we can do better than setting this globally
		zerolog.SetGlobalLevel(zerolog.Disabled)
	}

	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the account manager to handle account transactions.
	manager, err := NewAccountManager(ctx2, keys, encCfg, opts.masterAcc, conn, opts.pollTime, opts.useFeeGrant)
	if err != nil {
		return err
	}

	// Set custom gas limit if provided
	if opts.gasLimit > 0 {
		manager.SetGasLimit(opts.gasLimit)
	}

	// Set custom gas price if provided
	if opts.gasPrice > 0 {
		manager.SetGasPrice(opts.gasPrice)
	}

	// Initialize each of the sequences by allowing them to allocate accounts.
	for _, sequence := range sequences {
		sequence.Init(ctx2, manager.conn, manager.AllocateAccounts, r, opts.useFeeGrant)
	}

	// Generate the allotted accounts on chain by sending them sufficient funds
	if err := manager.GenerateAccounts(ctx2); err != nil {
		return err
	}

	errCh := make(chan error, len(sequences))
	var totalBytes int64

	// Spin up a task group to run each of the sequences concurrently.
	for idx, sequence := range sequences {
		go func(seqID int, sequence Sequence, errCh chan<- error) {
			opNum := 0
			r := rand.New(rand.NewSource(opts.seed))
			// each sequence loops through the next set of operations, the new messages are then
			// submitted on chain
			for {
				if ctx2.Err() != nil {
					errCh <- context.Canceled
					return
				}
				ops, err := sequence.Next(ctx2, manager.conn, r)
				if err != nil {
					errCh <- fmt.Errorf("sequence %d: %w", seqID, err)
					return
				}

				// Submit the messages to the chain.
				if err := manager.Submit(ctx2, ops); err != nil {
					errCh <- fmt.Errorf("sequence %d: %w", seqID, err)
					return
				}

				if opts.byteLimit > 0 {
					sz := opBytes(ops)
					if sz > 0 {
						cur := atomic.AddInt64(&totalBytes, sz)
						if cur >= opts.byteLimit {
							cancel()
							errCh <- ErrEndOfSequence
							return
						}
					}
				}
				opNum++
			}
		}(idx, sequence, errCh)
	}

	var finalErr error
	for range sequences {
		err := <-errCh
		if err == nil { // should never happen
			continue
		}
		if errors.Is(err, ErrEndOfSequence) {
			log.Info().Err(err).Msg("sequence terminated")
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		log.Error().Err(err).Msg("sequence failed")
		finalErr = err
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return finalErr
}

type Options struct {
	seed           int64
	masterAcc      string
	pollTime       time.Duration
	useFeeGrant    bool
	suppressLogger bool
	gasLimit       uint64
	gasPrice       float64
	byteLimit      int64
}

func (o *Options) Fill() {
	if o.seed == 0 {
		o.seed = DefaultSeed
	}
	if o.pollTime == 0 {
		o.pollTime = user.DefaultPollTime
	}
}

func DefaultOptions() *Options {
	opts := &Options{}
	opts.Fill()
	return opts
}

func (o *Options) SuppressLogs() *Options {
	o.suppressLogger = true
	return o
}

func (o *Options) UseFeeGrant() *Options {
	o.useFeeGrant = true
	return o
}

func (o *Options) SpecifyMasterAccount(name string) *Options {
	o.masterAcc = name
	return o
}

func (o *Options) WithSeed(seed int64) *Options {
	o.seed = seed
	return o
}

func (o *Options) WithPollTime(pollTime time.Duration) *Options {
	o.pollTime = pollTime
	return o
}

func (o *Options) WithGasLimit(gasLimit uint64) *Options {
	o.gasLimit = gasLimit
	return o
}

func (o *Options) WithGasPrice(gasPrice float64) *Options {
	o.gasPrice = gasPrice
	return o
}

func (o *Options) WithByteLimit(limit int64) *Options {
	o.byteLimit = limit
	return o
}

// buildGrpcConn applies the config if the handshake succeeds; otherwise, it falls back to an insecure connection.
func buildGrpcConn(grpcEndpoint string, config *tls.Config) (*grpc.ClientConn, error) {
	netConn, err := net.Dial("tcp", grpcEndpoint)
	if err != nil {
		log.Error().Str("errorMessage", err.Error()).Msg("grpc server is not reachable via tcp")
		return nil, err
	}

	tlsConn := tls.Client(netConn, config)
	err = tlsConn.Handshake()
	if err != nil {
		log.Warn().Str("errorMessage", err.Error()).Msg(
			"failed to connect with the config to grpc server; proceeding with insecure connection",
		)

		conn, err := grpc.NewClient(grpcEndpoint,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(grpcMaxRecvMsgSize), grpc.MaxCallSendMsgSize(grpcMaxSendMsgSize)))

		return conn, err
	}

	conn, err := grpc.NewClient(grpcEndpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(config)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(grpcMaxRecvMsgSize), grpc.MaxCallSendMsgSize(grpcMaxSendMsgSize)))
	if err != nil {
		return nil, fmt.Errorf("error connecting to %s: %w", grpcEndpoint, err)
	}
	return conn, err
}
