package txsim

import (
	"context"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/grpc"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

var _ Sequence = &StakeSequence{}

// StakeSequence sets up an endless sequence whereby an account delegates to a validator, continuously claims
// the reward, and occasionally redelegates to another validator at random. The account only ever delegates
// to a single validator at a time. TODO: Allow for multiple delegations
type StakeSequence struct {
	initialStake          int
	redelegateProbability int
	delegatedTo           string
	account               types.AccAddress
}

func NewStakeSequence(initialStake int) *StakeSequence {
	return &StakeSequence{
		initialStake:          initialStake,
		redelegateProbability: 10, // 1 in every 10
	}
}

func (s *StakeSequence) Clone(n int) []Sequence {
	sequenceGroup := make([]Sequence, n)
	for i := 0; i < n; i++ {
		sequenceGroup[i] = NewStakeSequence(s.initialStake)
	}
	return sequenceGroup
}

func (s *StakeSequence) Init(_ context.Context, _ grpc.ClientConn, allocateAccounts AccountAllocator, _ *rand.Rand, useFeegrant bool) {
	funds := fundsForGas
	if useFeegrant {
		funds = 1
	}
	s.account = allocateAccounts(1, s.initialStake+funds)[0]
}

func (s *StakeSequence) Next(ctx context.Context, querier grpc.ClientConn, rand *rand.Rand) (Operation, error) {
	var op Operation

	// for the first operation, the account delegates to a validator
	if s.delegatedTo == "" {
		val, err := getRandomValidator(ctx, querier, rand)
		if err != nil {
			return Operation{}, err
		}
		s.delegatedTo = val.OperatorAddress
		return Operation{
			Msgs: []types.Msg{
				&staking.MsgDelegate{
					DelegatorAddress: s.account.String(),
					ValidatorAddress: s.delegatedTo,
					Amount:           types.NewInt64Coin(appconsts.BondDenom, int64(s.initialStake)),
				},
			},
		}, nil
	}

	// occasionally redelegate the initial stake to another validator at random
	if rand.Intn(s.redelegateProbability) == 0 {
		val, err := getRandomValidator(ctx, querier, rand)
		if err != nil {
			return Operation{}, err
		}
		if val.OperatorAddress != s.delegatedTo {
			op = Operation{
				Msgs: []types.Msg{
					&staking.MsgBeginRedelegate{
						DelegatorAddress:    s.account.String(),
						ValidatorSrcAddress: s.delegatedTo,
						ValidatorDstAddress: val.OperatorAddress,
						// NOTE: only the initial stake is redelegated (not the entire balance)
						Amount: types.NewInt64Coin(appconsts.BondDenom, int64(s.initialStake)),
					},
				},
			}
			s.delegatedTo = val.OperatorAddress
			return op, nil
		}
	}

	// claim pending rewards
	op = Operation{
		Msgs: []types.Msg{
			&distribution.MsgWithdrawDelegatorReward{
				DelegatorAddress: s.account.String(),
				ValidatorAddress: s.delegatedTo,
			},
		},
		Delay: uint64(rand.Int63n(20)),
	}

	return op, nil
}

func getRandomValidator(ctx context.Context, conn grpc.ClientConn, rand *rand.Rand) (staking.Validator, error) {
	resp, err := staking.NewQueryClient(conn).Validators(ctx, &staking.QueryValidatorsRequest{})
	if err != nil {
		return staking.Validator{}, err
	}
	return resp.Validators[rand.Intn(len(resp.Validators))], nil
}
