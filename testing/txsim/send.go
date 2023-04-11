package txsim

import (
	"context"
	"math/rand"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/gogo/protobuf/grpc"
)

var _ Sequence = &SendSequence{}

// SendSequence sets up an endless sequence of send transactions, moving tokens
// between a set of accounts
type SendSequence struct {
	numAccounts    int
	sendAmount     int
	maxHeightDelay int
	accounts       []types.AccAddress
	sequence       int
}

func NewSendSequence(numAccounts int, sendAmount int) *SendSequence {
	return &SendSequence{
		numAccounts:    numAccounts,
		sendAmount:     sendAmount,
		maxHeightDelay: 5,
	}
}

func (s *SendSequence) Clone(n int) []Sequence {
	sequenceGroup := make([]Sequence, n)
	for i := 0; i < n; i++ {
		sequenceGroup[i] = NewSendSequence(s.numAccounts, s.sendAmount)
	}
	return sequenceGroup
}

func (s *SendSequence) Init(_ context.Context, _ grpc.ClientConn, allocateAccounts AccountAllocator, _ *rand.Rand) {
	s.accounts = allocateAccounts(s.numAccounts, s.sendAmount)
}

// Next sumbits a transaction to remove funds from one account to the next
func (s *SendSequence) Next(_ context.Context, _ grpc.ClientConn, rand *rand.Rand) (Operation, error) {
	op := Operation{
		Msgs: []types.Msg{
			bank.NewMsgSend(s.accounts[s.sequence%s.numAccounts], s.accounts[(s.sequence+1)%s.numAccounts], types.NewCoins(types.NewInt64Coin(app.BondDenom, int64(s.sendAmount)))),
		},
		Delay: rand.Int63n(int64(s.maxHeightDelay)),
	}
	s.sequence++
	return op, nil
}
