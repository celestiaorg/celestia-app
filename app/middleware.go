package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"

	authmiddleware "github.com/cosmos/cosmos-sdk/x/auth/middleware"
)

// ComposeMiddlewares compose multiple middlewares on top of a tx.Handler. The
// middleware order in the variadic arguments is from outer to inner.
//
// Example: Given a base tx.Handler H, and two middlewares A and B, the
// middleware stack:
// ```
// A.pre
//   B.pre
//     H
//   B.post
// A.post
// ```
// is created by calling `ComposeMiddlewares(H, A, B)`.
func ComposeMiddlewares(txHandler tx.Handler, middlewares ...tx.Middleware) tx.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		txHandler = middlewares[i](txHandler)
	}

	return txHandler
}

type TxHandlerOptions struct {
	Debug bool

	TxDecoder sdk.TxDecoder

	authmiddleware.TxHandlerOptions
	// IBCKeeper         *IBCKeeper.Keeper
	TXCounterStoreKey storetypes.StoreKey
	LegacyRouter      sdk.Router
	MsgServiceRouter  *authmiddleware.MsgServiceRouter
	IndexEvents       map[string]struct{}

	AccountKeeper   authmiddleware.AccountKeeper
	BankKeeper      types.BankKeeper
	FeegrantKeeper  authmiddleware.FeegrantKeeper
	SignModeHandler authsigning.SignModeHandler
	SigGasConsumer  func(meter sdk.GasMeter, sig signing.SignatureV2, params types.Params) error
}

// NewDefaultTxHandler defines a TxHandler middleware stacks that should work
// for most applications.
func NewDefaultTxHandler(options TxHandlerOptions) (tx.Handler, error) {
	if options.TxDecoder == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "txDecoder is required for middlewares")
	}

	if options.AccountKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "account keeper is required for middlewares")
	}

	if options.BankKeeper == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "bank keeper is required for middlewares")
	}

	if options.SignModeHandler == nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrLogic, "sign mode handler is required for middlewares")
	}

	sigGasConsumer := options.SigGasConsumer
	if sigGasConsumer == nil {
		sigGasConsumer = authmiddleware.DefaultSigVerificationGasConsumer
	}

	return ComposeMiddlewares(
		authmiddleware.NewRunMsgsTxHandler(options.MsgServiceRouter, options.LegacyRouter),
		authmiddleware.NewTxDecoderMiddleware(options.TxDecoder),
		// Set a new GasMeter on sdk.Context.
		//
		// Make sure the Gas middleware is outside of all other middlewares
		// that reads the GasMeter. In our case, the Recovery middleware reads
		// the GasMeter to populate GasInfo.
		authmiddleware.GasTxMiddleware,
		// Recover from panics. Panics outside of this middleware won't be
		// caught, be careful!
		authmiddleware.RecoveryTxMiddleware,
		// Choose which events to index in Tendermint. Make sure no events are
		// emitted outside of this middleware.
		authmiddleware.NewIndexEventsTxMiddleware(options.IndexEvents),
		// Reject all extension options which can optionally be included in the
		// tx.
		//		middleware.RejectExtensionOptionsMiddleware,
		// middleware.MempoolFeeMiddleware,
		authmiddleware.ValidateBasicMiddleware,
		authmiddleware.TxTimeoutHeightMiddleware,
		authmiddleware.ValidateMemoMiddleware(options.AccountKeeper),
		authmiddleware.ConsumeTxSizeGasMiddleware(options.AccountKeeper),
		// No gas should be consumed in any middleware above in a "post" handler part. See
		// ComposeMiddlewares godoc for details.
		// `DeductFeeMiddleware` and `IncrementSequenceMiddleware` should be put outside of `WithBranchedStore` middleware,
		// so their storage writes are not discarded when tx fails.
		authmiddleware.DeductFeeMiddleware(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker),
		//		middleware.TxPriorityMiddleware,
		authmiddleware.SetPubKeyMiddleware(options.AccountKeeper),
		authmiddleware.ValidateSigCountMiddleware(options.AccountKeeper),
		authmiddleware.SigGasConsumeMiddleware(options.AccountKeeper, sigGasConsumer),
		authmiddleware.SigVerificationMiddleware(options.AccountKeeper, options.SignModeHandler),
		authmiddleware.IncrementSequenceMiddleware(options.AccountKeeper),
		// Creates a new MultiStore branch, discards downstream writes if the downstream returns error.
		// These kinds of middlewares should be put under this:
		// - Could return error after messages executed successfully.
		// - Storage writes should be discarded together when tx failed.
		authmiddleware.WithBranchedStore,
		// Consume block gas. All middlewares whose gas consumption after their `next` handler
		// should be accounted for, should go below this middleware.
		authmiddleware.ConsumeBlockGasMiddleware,
		authmiddleware.NewTipMiddleware(options.BankKeeper),
		// Ibc v3 middleware
		// ibcmiddleware.IBCTxMiddleware(options.IBCKeeper),
	), nil
}
