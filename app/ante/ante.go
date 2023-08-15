package ante

import (
	blobante "github.com/celestiaorg/celestia-app/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/x/blob/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	ibcante "github.com/cosmos/ibc-go/v6/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v6/modules/core/keeper"
)

func NewAnteHandler(
	accountKeeper ante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	blobKeeper blob.Keeper,
	feegrantKeeper ante.FeegrantKeeper,
	signModeHandler signing.SignModeHandler,
	sigGasConsumer ante.SignatureVerificationGasConsumer,
	channelKeeper *ibckeeper.Keeper,
) sdk.AnteHandler {
	return sdk.ChainAnteDecorators(
		ante.NewSetUpContextDecorator(), // outermost AnteDecorator. SetUpContext must be called first
		// reject all tx extensions
		ante.NewExtensionOptionsDecorator(nil),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(accountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(accountKeeper),
		// check that the fee matches the gas and the local minimum gas price
		// of the validator
		ante.NewDeductFeeDecorator(accountKeeper, bankKeeper, feegrantKeeper, nil),
		ante.NewSetPubKeyDecorator(accountKeeper), // SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewValidateSigCountDecorator(accountKeeper),
		ante.NewSigGasConsumeDecorator(accountKeeper, sigGasConsumer),
		ante.NewSigVerificationDecorator(accountKeeper, signModeHandler),
		blobante.NewMinGasPFBDecorator(blobKeeper),
		blobante.NewMaxBlobSizeDecorator(blobKeeper),
		NewGovProposalDecorator(),
		ante.NewIncrementSequenceDecorator(accountKeeper),
		ibcante.NewRedundantRelayDecorator(channelKeeper),
	)
}

var DefaultSigVerificationGasConsumer = ante.DefaultSigVerificationGasConsumer
