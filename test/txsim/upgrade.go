package txsim

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

	signaltypes "github.com/celestiaorg/celestia-app/v3/x/signal/types"
	"github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/gogo/protobuf/grpc"
)

var _ Sequence = &UpgradeSequence{}

const fundsForUpgrade = 100_000

// UpgradeSequence simulates a sequence of validators submitting
// MsgSignalVersions for a particular version and then eventually a
// MsgTryUpgrade.
type UpgradeSequence struct {
	// signalled is a map from validator address to a boolean indicating if they have signalled.
	signalled map[string]bool
	// height is the first height at which the upgrade sequence is run.
	height int64
	// version is the version that validators are signalling for.
	version uint64
	// account is the address of the account that submits the MsgTryUpgrade.
	account types.AccAddress
	// hasUpgraded is true if the MsgTryUpgrade has been submitted.
	hasUpgraded bool
}

func NewUpgradeSequence(version uint64, height int64) *UpgradeSequence {
	return &UpgradeSequence{version: version, height: height, signalled: make(map[string]bool)}
}

func (s *UpgradeSequence) Clone(_ int) []Sequence {
	panic("cloning not supported for upgrade sequence. Only a single sequence is needed")
}

// this is a no-op for the upgrade sequence
func (s *UpgradeSequence) Init(_ context.Context, _ grpc.ClientConn, allocateAccounts AccountAllocator, _ *rand.Rand, _ bool) {
	s.account = allocateAccounts(1, fundsForUpgrade)[0]
}

func (s *UpgradeSequence) Next(ctx context.Context, querier grpc.ClientConn, _ *rand.Rand) (Operation, error) {
	if s.hasUpgraded {
		return Operation{}, ErrEndOfSequence
	}

	stakingQuerier := stakingtypes.NewQueryClient(querier)
	validatorsResp, err := stakingQuerier.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	fmt.Printf("validators: %v\n", validatorsResp.Validators)
	if err != nil {
		return Operation{}, err
	}

	if len(validatorsResp.Validators) == 0 {
		return Operation{}, errors.New("no validators found")
	}

	delay := uint64(0)
	// apply a delay to the first signal only
	if len(s.signalled) == 0 {
		fmt.Printf("applying delay to first signal")
		delay = uint64(s.height)
	}

	// Choose a random validator to be the authority
	for _, validator := range validatorsResp.Validators {
		fmt.Printf("validator: %v\n", validator)
		if !s.signalled[validator.OperatorAddress] {
			fmt.Printf("marking %v as signalled", validator.OperatorAddress)
			s.signalled[validator.OperatorAddress] = true
			msg := &signaltypes.MsgSignalVersion{
				ValidatorAddress: validator.OperatorAddress,
				Version:          s.version,
			}
			return Operation{
				Msgs:  []types.Msg{msg},
				Delay: delay,
			}, nil
		}
	}

	// if all validators have voted, we can now try to upgrade.
	fmt.Printf("all validators have signalled so we can try to upgrade")
	s.hasUpgraded = true
	msg := signaltypes.NewMsgTryUpgrade(s.account)
	return Operation{
		Msgs:  []types.Msg{msg},
		Delay: delay,
	}, nil
}
