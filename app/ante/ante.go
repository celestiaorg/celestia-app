package ante

import (
	errors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/v3/x/minfee"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerror "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

// priorityScalingFactor is used to scale the gas price into transaction priority.
// The value 1_000_000 is chosen to ensure sufficient precision during conversion.
const (
	priorityScalingFactor = 1_000_000
)

// ValidateTxFeeWrapper creates a wrapper for transaction fee validation that conforms
// to the ante.TxFeeChecker interface. This allows passing the additional parameter
// paramKeeper into ante.NewDeductFeeDecorator.
//
// Parameters:
// - paramKeeper: parameter storage for accessing the minimum gas price
//
// Returns:
// - a function conforming to the ante.TxFeeChecker interface
func ValidateTxFeeWrapper(paramKeeper params.Keeper) ante.TxFeeChecker {
	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		return ValidateTxFee(ctx, tx, paramKeeper)
	}
}

// ValidateTxFee implements default fee validation logic for transactions.
// It ensures that the provided transaction fee meets a minimum threshold for the node
// as well as a network minimum threshold and computes the transaction priority based on the gas price.
func ValidateTxFee(ctx sdk.Context, tx sdk.Tx, paramKeeper params.Keeper) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errors.Wrap(sdkerror.ErrTxDecode, "Tx must be a FeeTx")
	}

	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
	gas := feeTx.GetGas()

	// Ensure that the provided fee meets a minimum threshold for the node.
	// This is only for local mempool purposes and thus
	// is only run during check tx.
	if ctx.IsCheckTx() {
		minGasPrice := ctx.MinGasPrices().AmountOf(appconsts.BondDenom)
		if !minGasPrice.IsZero() {
			err := verifyMinFee(fee, gas, minGasPrice, "insufficient minimum gas price for this node")
			if err != nil {
				return nil, 0, err
			}
		}
	}

	// Ensure that the provided fee meets a network minimum threshold.
	// Network minimum fee only applies to app versions greater than one.
	if ctx.BlockHeader().Version.App > v1.Version {
		subspace, exists := paramKeeper.GetSubspace(minfee.ModuleName)
		if !exists {
			return nil, 0, errors.Wrap(sdkerror.ErrInvalidRequest, "minfee is not a registered subspace")
		}

		if !subspace.Has(ctx, minfee.KeyNetworkMinGasPrice) {
			return nil, 0, errors.Wrap(sdkerror.ErrKeyNotFound, "NetworkMinGasPrice")
		}

		var networkMinGasPrice sdk.Dec
		// Gets the network minimum gas price from the parameter store.
		// Panics if not configured properly.
		subspace.Get(ctx, minfee.KeyNetworkMinGasPrice, &networkMinGasPrice)

		err := verifyMinFee(fee, gas, networkMinGasPrice, "insufficient gas price for the network")
		if err != nil {
			return nil, 0, err
		}
	}

	priority := getTxPriority(feeTx.GetFee(), int64(gas))
	return feeTx.GetFee(), priority, nil
}

// verifyMinFee checks that the provided transaction fee is sufficient
// given the minimum gas price.
//
// Parameters:
// - fee: the actual transaction fee
// - gas: the gas limit for the transaction
// - minGasPrice: the minimum gas price (can be set by the node or network)
// - errMsg: the error message to use if the fee is insufficient
//
// Returns:
// - error: an error if the fee is insufficient, or nil if the check passes
func verifyMinFee(fee math.Int, gas uint64, minGasPrice sdk.Dec, errMsg string) error {
	// Determine the required fee by multiplying the required minimum gas
	// price by the gas limit, where fee = minGasPrice * gas.
	minFee := minGasPrice.MulInt(sdk.NewIntFromUint64(gas)).Ceil()
	if fee.LT(minFee.TruncateInt()) {
		return errors.Wrapf(sdkerror.ErrInsufficientFee, "%s; got: %s required at least: %s", errMsg, fee, minFee)
	}
	return nil
}

// getTxPriority calculates the transaction priority based on the gas price.
// Priority is computed as (fee * priorityScalingFactor) / gas for each coin in the fee.
// If the transaction contains multiple types of coins, the lowest priority is used.
//
// Parameters:
// - fee: the transaction fee as a set of coins
// - gas: the gas limit for the transaction
//
// Returns:
// - int64: the transaction priority, where a higher value indicates a higher priority
func getTxPriority(fee sdk.Coins, gas int64) int64 {
	var priority int64
	for _, c := range fee {
		p := c.Amount.Mul(sdk.NewInt(priorityScalingFactor)).QuoRaw(gas)
		if !p.IsInt64() {
			continue
		}
		// take the lowest priority as the transaction priority
		if priority == 0 || p.Int64() < priority {
			priority = p.Int64()
		}
	}

	return priority
}
