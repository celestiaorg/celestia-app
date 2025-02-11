package txsim

import (
	"context"
	"errors"
	"math/rand"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/grpc"
)

// Sequence is the basic unit for programmatic transaction generation.
// It embodies a pattern of transactions which are executed among a group
// of accounts in isolation from the rest of the state machine.
type Sequence interface {
	// Clone replicates n instances of the sequence for scaling up the load
	// on a network. This is called before `Init`
	Clone(n int) []Sequence

	// Init allows the sequence to initialize itself. It may read the current state of
	// the chain and provision accounts for usage throughout the sequence.
	// For any randomness, use the rand source provided.
	Init(ctx context.Context, querier grpc.ClientConn, accountAllocator AccountAllocator, rand *rand.Rand, useFeegrant bool)

	// Next returns the next operation in the sequence. It returns EndOfSequence
	// when the sequence has been exhausted. The sequence may make use of the
	// grpc connection to query the state of the network as well as the deterministic
	// random number generator. Any error will abort the rest of the sequence.
	Next(ctx context.Context, querier grpc.ClientConn, rand *rand.Rand) (Operation, error)
}

// Operation represents a series of messages and blobs that are to be bundled
// in a single transaction. A delay (in heights) may also be set before the transaction is sent.
// The gas limit and price can also be set. If left at 0, the DefaultGasLimit will be used.
type Operation struct {
	Msgs     []types.Msg
	Blobs    []*share.Blob
	Delay    uint64
	GasLimit uint64
	GasPrice float64
}

const (
	// Set the default gas limit to cover the costs of most transactions.
	// At 0.1 utia per gas, this equates to 20_000utia per transaction.
	DefaultGasLimit = 200_000
)

// ErrEndOfSequence is a special error which indicates that the sequence has been terminated
var ErrEndOfSequence = errors.New("end of sequence")

// AccountAllocator reserves and funds a series of accounts to be used exclusively by
// the Sequence.
type AccountAllocator func(n, balance int) []types.AccAddress
