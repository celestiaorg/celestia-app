package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.HasValidateBasic = (*MsgCreateInterchainSecurityModule)(nil)
	_ sdk.HasValidateBasic = (*MsgSubmitAttestation)(nil)
)

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgCreateInterchainSecurityModule) ValidateBasic() error {
	if len(msg.State) < MinStateBytes {
		return errorsmod.Wrapf(ErrInvalidTrustedState, "initial trusted state must be at least %d bytes", MinStateBytes)
	}
	if len(msg.State) > MaxStateBytes {
		return errorsmod.Wrapf(ErrInvalidTrustedState, "initial trusted state must be no greater than %d bytes", MaxStateBytes)
	}
	// This module writes and reads the full 116-byte layout, so an initial state
	// of any other length could never be advanced.
	if len(msg.State) != IsmStateBytes {
		return errorsmod.Wrapf(ErrInvalidTrustedState, "initial trusted state must be exactly %d bytes", IsmStateBytes)
	}
	if len(msg.MerkleTreeAddress) != 32 {
		return errorsmod.Wrap(ErrInvalidMerkleTreeAddress, "merkle tree address must be 32 bytes")
	}
	if err := msg.Identity.Validate(); err != nil {
		return err
	}

	// The state names the enclave it was created for, so a mismatch here would
	// leave an ISM that rejects the very first attestation.
	state, err := DecodeIsmState(msg.State)
	if err != nil {
		return errorsmod.Wrap(ErrInvalidTrustedState, err.Error())
	}
	if state.IdentityDigest != msg.Identity.Digest() {
		return errorsmod.Wrapf(ErrInvalidEnclaveIdentity,
			"state names enclave %x but the pinned identity digests to %x",
			state.IdentityDigest[:], msg.Identity.Digest())
	}
	return nil
}

// ValidateBasic implements stateless validation for the HasValidateBasic interface.
func (msg *MsgSubmitAttestation) ValidateBasic() error {
	if msg.Id.IsZeroAddress() {
		return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "ism identifier must be non-zero")
	}
	for _, f := range []struct {
		name  string
		size  int
		limit int
	}{
		{"quote", len(msg.Quote), MaxQuoteBytes},
		{"event_log", len(msg.EventLog), MaxEventLogBytes},
		{"payload", len(msg.Payload), MaxPayloadBytes},
	} {
		if f.size == 0 {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s must not be empty", f.name)
		}
		if f.size > f.limit {
			return errorsmod.Wrapf(ErrMessageTooLarge, "%s is %d bytes, limit is %d", f.name, f.size, f.limit)
		}
	}
	if err := msg.Collateral.Validate(); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		val  []byte
	}{
		{"pck_crl", msg.Collateral.PckCrl},
		{"pck_crl_issuer_chain", msg.Collateral.PckCrlIssuerChain},
		{"tcb_info", msg.Collateral.TcbInfo},
		{"tcb_info_issuer_chain", msg.Collateral.TcbInfoIssuerChain},
		{"qe_identity", msg.Collateral.QeIdentity},
		{"qe_identity_issuer_chain", msg.Collateral.QeIdentityIssuerChain},
		{"root_ca_crl", msg.Collateral.RootCaCrl},
	} {
		if len(f.val) > MaxCollateralFieldBytes {
			return errorsmod.Wrapf(ErrMessageTooLarge, "collateral %s is %d bytes, limit is %d", f.name, len(f.val), MaxCollateralFieldBytes)
		}
	}
	return nil
}
