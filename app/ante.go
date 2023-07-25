package app

import (
	"github.com/celestiaorg/celestia-app/tools/gasmonitor"
	blobante "github.com/celestiaorg/celestia-app/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/x/blob/keeper"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	ibcante "github.com/cosmos/ibc-go/v6/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v6/modules/core/keeper"
)

func NewAnteHandler(
	appOpts servertypes.AppOptions,
	accountKeeper ante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	blobKeeper blob.Keeper,
	feegrantKeeper ante.FeegrantKeeper,
	signModeHandler signing.SignModeHandler,
	sigGasConsumer ante.SignatureVerificationGasConsumer,
	channelKeeper *ibckeeper.Keeper,
) sdk.AnteHandler {
	decorators := []sdk.AnteDecorator{
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
		ante.NewIncrementSequenceDecorator(accountKeeper),
		ibcante.NewRedundantRelayDecorator(channelKeeper),
	}

	// record a gas consumption trace for each transaction executed. see
	// tools/gasmonitor for more details
	if gasConsumuptionMonitor := appOpts.Get(gasmonitor.AppOptionsKey); gasConsumuptionMonitor != nil {
		gcm := gasConsumuptionMonitor.(*gasmonitor.Decorator)
		decorators = append(decorators, nil)
		copy(decorators[2:], decorators[1:])
		decorators[1] = gcm
	}

	return sdk.ChainAnteDecorators(decorators...)
}
