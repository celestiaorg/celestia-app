package ante

import (
	circuitante "cosmossdk.io/x/circuit/ante"
	circuitkeeper "cosmossdk.io/x/circuit/keeper"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	blobante "github.com/celestiaorg/celestia-app/v10/x/blob/ante"
	blob "github.com/celestiaorg/celestia-app/v10/x/blob/keeper"
	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	fibrekeeper "github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	minfeekeeper "github.com/celestiaorg/celestia-app/v10/x/minfee/keeper"
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
	fibreKeeper *fibrekeeper.Keeper,
	sigCache *sigcache.Cache,
) sdk.AnteHandler {
	return newAppAnteChain(
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
		// Ensure the feepayer (fee granter or first signer) has enough funds to pay for the tx.
		// Ensure that the tx's gas price is >= the network minimum gas price.
		// Side effect: deducts fees from the fee payer. Sets the tx priority in context.
		ante.NewDeductFeeDecorator(accountKeeper, bankKeeper, feegrantKeeper, ValidateTxFeeWrapper(minfeeKeeper)),
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
		// Note: does not consume gas from the gas meter. Skips the curve
		// operation for signatures a previous ante pass already verified.
		NewCachedSigVerificationDecorator(accountKeeper, signModeHandler, sigCache),
		// Reject MsgPayForBlobs, MsgPayForFibre, MsgExec, or MsgSubmitProposal
		// wrapped inside a MsgExec or MsgSubmitProposal.
		// Proposal PFF: inert, it has no MsgExec or MsgSubmitProposal.
		inertForProposalPFF{NewNestedMsgDecorator()},
		// Charge deterministic gas for MsgPayForFibre checks.
		fibreante.NewFibreSignatureGasDecorator(),
		// Ensure that the tx's gas limit is > the gas consumed based on the blob size(s).
		// Contract: must be called after all decorators that consume gas.
		// Note: does not consume gas from the gas meter.
		// Proposal PFF: inert, it has no MsgPayForBlobs.
		inertForProposalPFF{blobante.NewMinGasPFBDecorator()},
		// Ensure that the blob shares occupied by the tx <= the max shares
		// available to blob data in a data square.
		// Proposal PFF: inert, it only runs in CheckTx.
		inertForProposalPFF{blobante.NewBlobShareDecorator(blobKeeper)},
		// Reject unsettleable MsgPayForFibre txs in CheckTx and recheck to keep
		// replayed or stale promises out of the mempool.
		// Proposal PFF: inert, it only runs in CheckTx and recheck.
		inertForProposalPFF{fibreante.NewFibreStatefulValidationDecorator(fibreKeeper)},
		// Verify uncached MsgPayForFibre validator signatures.
		fibreante.NewFibreSigVerificationDecorator(fibreKeeper, sigCache),
		// Ensure that txs with MsgSubmitProposal/MsgExec have at least one message and param filters are applied.
		// Proposal PFF: inert, it has no MsgExec or MsgSubmitProposal.
		inertForProposalPFF{NewParamFilterDecorator(paramFilters)},
		// Side effect: increment the nonce for all tx signers.
		ante.NewIncrementSequenceDecorator(accountKeeper),
		// Ensure that the tx is not an IBC packet or update message that has already been processed.
		// Proposal PFF: inert, it only runs in CheckTx and recheck.
		inertForProposalPFF{ibcante.NewRedundantRelayDecorator(channelKeeper)},
	)
}

var DefaultSigVerificationGasConsumer = ante.DefaultSigVerificationGasConsumer

// inertForProposalPFF marks a decorator that does nothing - no check, write
// or gas - for a single-message PayForFibre in ProcessProposal, outside
// CheckTx and recheck. The flat walk in newAppAnteChain leaves it out; every
// other path runs it unwrapped. Mark a decorator here, next to its
// construction, only with the reason written beside it.
type inertForProposalPFF struct {
	sdk.AnteDecorator
}

// newAppAnteChain is sdk.ChainAnteDecorators(panicHandler, setUp, chain...),
// except for a single-message PayForFibre in ProcessProposal: that runs chain
// as a flat loop inside the two wrappers, without the decorators marked
// inertForProposalPFF. The flat loop hands every decorator a terminal next, so
// no decorator in chain may do work after calling next, and each must call next
// unless it returns an error: one that returns early without it stops the
// regular chain but not the loop.
func newAppAnteChain(panicHandler HandlePanicDecorator, setUp ante.SetUpContextDecorator, chain ...sdk.AnteDecorator) sdk.AnteHandler {
	all := []sdk.AnteDecorator{panicHandler, setUp}
	var pffChain []sdk.AnteDecorator
	for _, decorator := range chain {
		if inert, ok := decorator.(inertForProposalPFF); ok {
			all = append(all, inert.AnteDecorator)
			continue
		}
		all = append(all, decorator)
		pffChain = append(pffChain, decorator)
	}
	regular := sdk.ChainAnteDecorators(all...)

	terminal := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	}
	walk := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		for _, decorator := range pffChain {
			var err error
			ctx, err = decorator.AnteHandle(ctx, tx, simulate, terminal)
			if err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	}
	setup := func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		return setUp.AnteHandle(ctx, tx, simulate, walk)
	}
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if ctx.ExecMode() != sdk.ExecModeProcessProposal || ctx.IsCheckTx() || ctx.IsReCheckTx() || fibretypes.PayForFibreMsg(tx) == nil {
			return regular(ctx, tx, simulate)
		}
		return panicHandler.AnteHandle(ctx, tx, simulate, setup)
	}
}
