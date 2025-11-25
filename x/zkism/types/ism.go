package types

import (
	"bytes"
	"context"
	"errors"

	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	InterchainSecurityModuleTypeZKExecution     = 42
	InterchainSecurityModuleTypeStateTransition = 43
)

var (
	_ ismtypes.HyperlaneInterchainSecurityModule = (*EvolveEvmISM)(nil)
	_ ismtypes.HyperlaneInterchainSecurityModule = (*ConsensusISM)(nil)
)

// GetId implements types.HyperlaneInterchainSecurityModule.
func (ism *EvolveEvmISM) GetId() (util.HexAddress, error) {
	if ism.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}

	return ism.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (ism *EvolveEvmISM) ModuleType() uint8 {
	return InterchainSecurityModuleTypeZKExecution
}

// Verify implements types.HyperlaneInterchainSecurityModule.
func (ism *EvolveEvmISM) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return false, sdkerrors.ErrNotSupported
}

// GetId implements types.HyperlaneInterchainSecurityModule.
func (v *ConsensusISM) GetId() (util.HexAddress, error) {
	if v.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}

	return v.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (v *ConsensusISM) ModuleType() uint8 {
	return InterchainSecurityModuleTypeStateTransition
}

// Verify implements types.HyperlaneInterchainSecurityModule.
func (v *ConsensusISM) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return false, sdkerrors.ErrNotSupported
}

// HeaderHashGetter is an interface for retrieving header hashes by height.
type HeaderHashGetter interface {
	GetHeaderHash(ctx context.Context, height uint64) ([]byte, error)
}

// ValidatePublicValues validates the public values for ConsensusISM state transitions.
func (ism *ConsensusISM) ValidatePublicValues(ctx context.Context, publicValues StateTransitionPublicValues) error {
	if !bytes.Equal(publicValues.TrustedState, ism.TrustedState) {
		return errorsmod.Wrapf(ErrInvalidStateRoot, "expected %x, got %x", ism.TrustedState, publicValues.TrustedState)
	}
	return nil
}

// ValidatePublicValues validates the public values for EvolveEvmISM state transitions.
func (ism *EvolveEvmISM) ValidatePublicValues(ctx context.Context, publicValues EvExecutionPublicValues, headerHashGetter HeaderHashGetter) error {
	headerHash, err := headerHashGetter.GetHeaderHash(ctx, publicValues.CelestiaHeight)
	if err != nil {
		return errorsmod.Wrapf(ErrHeaderHashNotFound, "failed to get header for height %d", publicValues.CelestiaHeight)
	}

	if !bytes.Equal(headerHash, publicValues.CelestiaHeaderHash[:]) {
		return errorsmod.Wrapf(ErrInvalidHeaderHash, "expected %x, got %x", headerHash, publicValues.CelestiaHeaderHash[:])
	}

	if !bytes.Equal(publicValues.TrustedStateRoot[:], ism.StateRoot) {
		return errorsmod.Wrapf(ErrInvalidStateRoot, "expected %x, got %x", ism.StateRoot, publicValues.TrustedStateRoot)
	}

	if publicValues.PrevCelestiaHeight != ism.CelestiaHeight {
		return errorsmod.Wrapf(ErrInvalidHeight, "expected %d, got %d", ism.CelestiaHeight, publicValues.PrevCelestiaHeight)
	}

	if !bytes.Equal(publicValues.PrevCelestiaHeaderHash[:], ism.CelestiaHeaderHash) {
		return errorsmod.Wrapf(ErrInvalidHeaderHash, "expected %x, got %x", ism.CelestiaHeaderHash, publicValues.PrevCelestiaHeaderHash)
	}

	if publicValues.TrustedHeight != ism.Height {
		return errorsmod.Wrapf(ErrInvalidHeight, "expected %d, got %d", ism.Height, publicValues.TrustedHeight)
	}

	if !bytes.Equal(publicValues.Namespace[:], ism.Namespace) {
		return errorsmod.Wrapf(ErrInvalidNamespace, "expected %x, got %x", ism.Namespace, publicValues.Namespace)
	}

	if !bytes.Equal(publicValues.PublicKey[:], ism.SequencerPublicKey) {
		return errorsmod.Wrapf(ErrInvalidSequencerKey, "expected %x, got %x", ism.SequencerPublicKey, publicValues.PublicKey)
	}

	return nil
}
