package types

import (
	"errors"
	"fmt"

	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.HasValidateBasic = (*MsgCreateZKExecutionISM)(nil)

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgCreateZKExecutionISM) ValidateBasic() error {
	if _, err := share.NewNamespaceFromBytes(msg.Namespace); err != nil {
		return fmt.Errorf("failed to parse namespace from bytes: %x", msg.Namespace)
	}

	if len(msg.SequencerPublicKey) != 32 {
		return errors.New("public key must be exactly 32 bytes")
	}

	if len(msg.StateRoot) != 32 {
		return errors.New("state root must be exactly 32 bytes")
	}

	if len(msg.VkeyCommitment) != 32 {
		return errors.New("vkey commitment must be exactly 32 bytes")
	}

	return nil
}
