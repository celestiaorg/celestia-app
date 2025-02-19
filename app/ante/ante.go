package ante

import (
	circuitante "cosmossdk.io/x/circuit/ante"
	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	txsigning "cosmossdk.io/x/tx/signing"
	blobante "github.com/celestiaorg/celestia-app/v4/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v4/x/blob/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	paramkeeper "github.com/cosmos/cosmos-sdk/x/params/keeper"
	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v8/modules/core/keeper"
)

func NewAnteHandler(
	accountKeeper ante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	blobKeeper blob.Keeper,
	feegrantKeeper ante.FeegrantKeeper,
	signModeHandler *txsigning.HandlerMap,
	sigGasConsumer ante.SignatureVerificationGasConsumer,
	channelKeeper *ibckeeper.Keeper,
	paramKeeper paramkeeper.Keeper,
	circuitkeeper *circuitkeeper.Keeper,
	forbiddenGovUpdateParams map[string][]string,
) sdk.AnteHandler {
	return sdk.ChainAnteDecorators(
		// Wraps the panic with the string format of the transaction
		NewHandlePanicDecorator(),
		// Set up the context with a gas meter.
		// Must be called before gas consumption occurs in any other decorator.
		ante.NewSetUpContextDecorator(),
		// Ensure the tx is not larger than the configured threshold.
		NewMaxTxSizeDecorator(),
		// Ensure the tx does not contain any extension options.
		ante.NewExtensionOptionsDecorator(nil),
		// Ensure the tx passes ValidateBasic.
		ante.NewValidateBasicDecorator(),
		// Ensure the tx has not reached a height timeout.
		ante.NewTxTimeoutHeightDecorator(),
		// Ensure the tx memo <= max memo characters.
		ante.NewValidateMemoDecorator(accountKeeper),
		// Ensure the tx's gas limit is > the gas consumed based on the tx size.
		// Side effect: consumes gas from the gas meter.
		NewConsumeGasForTxSizeDecorator(accountKeeper),
		// Ensure the feepayer (fee granter or first signer) has enough funds to pay for the tx.
		// Ensure the gas price >= network min gas price if app version >= 2.
		// Side effect: deducts fees from the fee payer. Sets the tx priority in context.
		ante.NewDeductFeeDecorator(accountKeeper, bankKeeper, feegrantKeeper, ValidateTxFeeWrapper(paramKeeper)),
		// Set public keys in the context for fee-payer and all signers.
		// Contract: must be called before all signature verification decorators.
		ante.NewSetPubKeyDecorator(accountKeeper),
		// Ensure that the tx's count of signatures is <= the tx signature limit.
		ante.NewValidateSigCountDecorator(accountKeeper),
		// Ensure that the tx's gas limit is > the gas consumed based on signature verification.
		// Side effect: consumes gas from the gas meter.
		ante.NewSigGasConsumeDecorator(accountKeeper, sigGasConsumer),
		// Ensure that the tx's signatures are valid. For each signature, ensure
		// that the signature's sequence number (a.k.a nonce) matches the
		// account sequence number of the signer.
		// Note: does not consume gas from the gas meter.
		ante.NewSigVerificationDecorator(accountKeeper, signModeHandler),
		// Ensure that the tx's gas limit is > the gas consumed based on the blob size(s).
		// Contract: must be called after all decorators that consume gas.
		// Note: does not consume gas from the gas meter.
		blobante.NewMinGasPFBDecorator(blobKeeper),
		// Ensure that the tx's total blob size is <= the max blob size.
		// Only applies to app version == 1.
		blobante.NewMaxTotalBlobSizeDecorator(blobKeeper),
		// Ensure that the blob shares occupied by the tx <= the max shares
		// available to blob data in a data square. Only applies to app version
		// >= 2.
		blobante.NewBlobShareDecorator(blobKeeper),
		// Ensure that tx's with a MsgSubmitProposal have at least one proposal
		// message.
		NewGovProposalDecorator(forbiddenGovUpdateParams),
		// Side effect: increment the nonce for all tx signers.
		ante.NewIncrementSequenceDecorator(accountKeeper),
		// Ensure that the tx is not an IBC packet or update message that has already been processed.
		ibcante.NewRedundantRelayDecorator(channelKeeper),
		// Wire circuit breaker decorator.
		circuitante.NewCircuitBreakerDecorator(circuitkeeper),
	)
}

var DefaultSigVerificationGasConsumer = ante.DefaultSigVerificationGasConsumer
