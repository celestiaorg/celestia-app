package txsim

import (
	"context"
	"errors"
	"math/rand"

	signaltypes "github.com/celestiaorg/celestia-app/v3/x/signal/types"
	"github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/gogo/protobuf/grpc"
)

var _ Sequence = &UpgradeSequence{}

const fundsForUpgrade = 100_000

// UpgradeSequence simulates an upgrade proposal and voting process
type UpgradeSequence struct {
	voted       map[string]bool
	height      int64
	version     uint64
	account     types.AccAddress
	hasUpgraded bool
}

func NewUpgradeSequence(version uint64, height int64) *UpgradeSequence {
	return &UpgradeSequence{version: version, height: height, voted: make(map[string]bool)}
}

func (s *UpgradeSequence) Clone(n int) []Sequence {
	panic("cloning not supported for upgrade sequence. Only a single sequence is needed")
}

// this is a no-op for the upgrade sequence
func (s *UpgradeSequence) Init(_ context.Context, _ grpc.ClientConn, allocateAccounts AccountAllocator, _ *rand.Rand, useFeegrant bool) {
	s.account = allocateAccounts(1, fundsForUpgrade)[0]
}

func (s *UpgradeSequence) Next(ctx context.Context, querier grpc.ClientConn, rand *rand.Rand) (Operation, error) {
	if s.hasUpgraded {
		return Operation{}, ErrEndOfSequence
	}

	stakingQuerier := stakingtypes.NewQueryClient(querier)
	validatorsResp, err := stakingQuerier.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	if err != nil {
		return Operation{}, err
	}

	if len(validatorsResp.Validators) == 0 {
		return Operation{}, errors.New("no validators found")
	}

	// Choose a random validator to be the authority
	var msg types.Msg
	for _, validator := range validatorsResp.Validators {
		if !s.voted[validator.OperatorAddress] {
			msg = &signaltypes.MsgSignalVersion{
				ValidatorAddress: validator.OperatorAddress,
				Version:          s.version,
			}
			s.voted[validator.OperatorAddress] = true
		}
	}
	// if all validators have voted, we can now try to upgrade.
	if msg == nil {
		msg = signaltypes.NewMsgTryUpgrade(s.account)
		s.hasUpgraded = true
	}

	delay := uint64(0)
	// apply a delay to the first sequence only
	if len(s.voted) == 0 {
		delay = uint64(s.height)
	}

	return Operation{
		Msgs:  []types.Msg{msg},
		Delay: delay,
	}, nil
}
