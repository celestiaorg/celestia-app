package groth16

import (
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	commitmenttypes "github.com/cosmos/ibc-go/v6/modules/core/23-commitment/types"
	"github.com/cosmos/ibc-go/v6/modules/core/exported"
)

var _ exported.ConsensusState = (*ConsensusState)(nil)

// NewConsensusState creates a new ConsensusState instance.
func NewConsensusState(
	timestamp time.Time, stateRoot []byte,
) *ConsensusState {
	return &ConsensusState{
		Timestamp: timestamp,
		StateRoot: stateRoot,
	}
}

// ClientType returns Tendermint
func (ConsensusState) ClientType() string {
	return exported.Tendermint
}

// GetRoot returns the commitment Root for the specific
func (cs ConsensusState) GetRoot() exported.Root {
	return commitmenttypes.NewMerkleRoot(cs.StateRoot)
}

// GetTimestamp returns block time in nanoseconds of the header that created consensus state
func (cs ConsensusState) GetTimestamp() uint64 {
	return uint64(cs.Timestamp.UnixNano())
}

func (cs ConsensusState) ValidateBasic() error {
	return nil
}

func (cs ConsensusState) IsExpired(blockTime time.Time) bool {
	return cs.Timestamp.Add(appconsts.DefaultUnbondingTime).After(blockTime)
}
