package ante

import (
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

var _ sdk.AnteDecorator = MultisigISMValidatorsDecorator{}

// MultisigISMValidatorsDecorator rejects multisig ISMs with more than
// MaxMultisigISMValidators validators. Every verification decodes the full
// stored set, but the per-block MaxISMSignatures budget only counts signatures,
// so an oversized ISM would make each MsgProcessMessage that uses it expensive.
// Only one level of MsgExec and MsgSubmitProposal is inspected because
// NestedMsgDecorator rejects deeper nesting, and the default ICA allowlist does
// not carry these messages.
type MultisigISMValidatorsDecorator struct{}

func NewMultisigISMValidatorsDecorator() MultisigISMValidatorsDecorator {
	return MultisigISMValidatorsDecorator{}
}

func (MultisigISMValidatorsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if err := checkMultisigValidators(msg); err != nil {
			return ctx, err
		}
		var nested []sdk.Msg
		var err error
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err = m.GetMessages()
		case *govv1.MsgSubmitProposal:
			nested, err = m.GetMsgs()
		}
		if err != nil {
			return ctx, err
		}
		for _, nestedMsg := range nested {
			if err := checkMultisigValidators(nestedMsg); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}

func checkMultisigValidators(msg sdk.Msg) error {
	var validators int
	switch m := msg.(type) {
	case *ismtypes.MsgCreateMessageIdMultisigIsm:
		validators = len(m.Validators)
	case *ismtypes.MsgCreateMerkleRootMultisigIsm:
		validators = len(m.Validators)
	default:
		return nil
	}
	if validators > appconsts.MaxMultisigISMValidators {
		return sdkerrors.ErrInvalidRequest.Wrapf("multisig ISM has %d validators, limit is %d", validators, appconsts.MaxMultisigISMValidators)
	}
	return nil
}
