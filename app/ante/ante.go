package ante

import (
	circuitante "cosmossdk.io/x/circuit/ante"
	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	txsigning "cosmossdk.io/x/tx/signing"
	blobante "github.com/celestiaorg/celestia-app/v7/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v7/x/blob/keeper"
	minfeekeeper "github.com/celestiaorg/celestia-app/v7/x/minfee/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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
	minfeeKeeper *minfeekeeper.Keeper,
	circuitkeeper *circuitkeeper.Keeper,
	paramFilters map[string]ParamFilter,
) sdk.AnteHandler {
	return sdk.ChainAnteDecorators(
		// Wraps the panic with the string format of the transaction
		NewHandlePanicDecorator(),
		// Set up the context with a gas meter.
		// Must be called before gas consumption occurs in any other decorator.
		ante.NewSetUpContextDecorator(),
		// Ensure that the tx does not contain any messages that are disabled by the circuit breaker.
		circuitante.NewCircuitBreakerDecorator(circuitkeeper),
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
		// Handle MsgForwardFees transactions: validate proposer, deduct fee from fee address,
		// and set context flag to skip signature-related decorators.
		// Must be called before DeductFeeDecorator.
		NewFeeForwardDecorator(bankKeeper),
		// Ensure the feepayer (fee granter or first signer) has enough funds to pay for the tx.
		// Ensure that the tx's gas price is >= the network minimum gas price.
		// Side effect: deducts fees from the fee payer. Sets the tx priority in context.
		// Skipped for fee forward transactions (fee already deducted by FeeForwardDecorator).
		NewSkipForFeeForwardDecorator(ante.NewDeductFeeDecorator(accountKeeper, bankKeeper, feegrantKeeper, ValidateTxFeeWrapper(minfeeKeeper))),
		// Set public keys in the context for fee-payer and all signers.
		// Contract: must be called before all signature verification decorators.
		// Skipped for fee forward transactions (no signers).
		NewSkipForFeeForwardDecorator(ante.NewSetPubKeyDecorator(accountKeeper)),
		// Ensure that the tx's count of signatures is <= the tx signature limit.
		// Skipped for fee forward transactions (no signatures).
		NewSkipForFeeForwardDecorator(ante.NewValidateSigCountDecorator(accountKeeper)),
		// Ensure that the tx's gas limit is > the gas consumed based on signature verification.
		// Side effect: consumes gas from the gas meter.
		// Skipped for fee forward transactions (no signatures).
		NewSkipForFeeForwardDecorator(ante.NewSigGasConsumeDecorator(accountKeeper, sigGasConsumer)),
		// Ensure that the tx's signatures are valid. For each signature, ensure
		// that the signature's sequence number (a.k.a nonce) matches the
		// account sequence number of the signer.
		// Note: does not consume gas from the gas meter.
		// Skipped for fee forward transactions (no signatures).
		NewSkipForFeeForwardDecorator(ante.NewSigVerificationDecorator(accountKeeper, signModeHandler)),
		// Ensure that the tx does not contain a MsgExec with a nested MsgExec
		// or MsgPayForBlobs.
		NewMsgExecDecorator(),
		// Ensure that only utia can be sent to the fee address.
		NewFeeAddressDecorator(),
		// Ensure that the tx's gas limit is > the gas consumed based on the blob size(s).
		// Contract: must be called after all decorators that consume gas.
		// Note: does not consume gas from the gas meter.
		blobante.NewMinGasPFBDecorator(blobKeeper),
		// Ensure that the blob shares occupied by the tx <= the max shares
		// available to blob data in a data square.
		blobante.NewBlobShareDecorator(blobKeeper),
		// Ensure that txs with MsgSubmitProposal/MsgExec have at least one message and param filters are applied.
		NewParamFilterDecorator(paramFilters),
		// Side effect: increment the nonce for all tx signers.
		// Skipped for fee forward transactions (no signers).
		NewSkipForFeeForwardDecorator(ante.NewIncrementSequenceDecorator(accountKeeper)),
		// Ensure that the tx is not an IBC packet or update message that has already been processed.
		ibcante.NewRedundantRelayDecorator(channelKeeper),
	)
}

var DefaultSigVerificationGasConsumer = ante.DefaultSigVerificationGasConsumer
