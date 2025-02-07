package ante

import (
	errors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/v4/x/minfee"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerror "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

const (
	// priorityScalingFactor is a scaling factor to convert the gas price to a priority.
	priorityScalingFactor = 1_000_000
)

// The purpose of this wrapper is to enable the passing of an additional paramKeeper parameter in
// ante.NewDeductFeeDecorator whilst still satisfying the ante.TxFeeChecker type.
func ValidateTxFeeWrapper(paramKeeper params.Keeper) ante.TxFeeChecker {
	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		return ValidateTxFee(ctx, tx, paramKeeper)
	}
}

// ValidateTxFee implements default fee validation logic for transactions.
// It ensures that the provided transaction fee meets a minimum threshold for the node
// as well as a network minimum threshold and computes the tx priority based on the gas price.
func ValidateTxFee(ctx sdk.Context, tx sdk.Tx, paramKeeper params.Keeper) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errors.Wrap(sdkerror.ErrTxDecode, "Tx must be a FeeTx")
	}

	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
	gas := feeTx.GetGas()

	// Ensure that the provided fee meets a minimum threshold for the node.
	// This is only for local mempool purposes, and thus
	// is only ran on check tx.
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

		var networkMinGasPrice math.LegacyDec
		// Gets the network minimum gas price from the param store.
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

// verifyMinFee validates that the provided transaction fee is sufficient given the provided minimum gas price.
func verifyMinFee(fee math.Int, gas uint64, minGasPrice math.LegacyDec, errMsg string) error {
	// Determine the required fee by multiplying required minimum gas
	// price by the gas limit, where fee = minGasPrice * gas.
	minFee := minGasPrice.MulInt(math.NewIntFromUint64(gas)).Ceil()
	if fee.LT(minFee.TruncateInt()) {
		return errors.Wrapf(sdkerror.ErrInsufficientFee, "%s; got: %s required at least: %s", errMsg, fee, minFee)
	}
	return nil
}

// getTxPriority returns a naive tx priority based on the amount of the smallest denomination of the gas price
// provided in a transaction.
// NOTE: This implementation should not be used for txs with multiple coins.
func getTxPriority(fee sdk.Coins, gas int64) int64 {
	var priority int64
	for _, c := range fee {
		p := c.Amount.Mul(math.NewInt(priorityScalingFactor)).QuoRaw(gas)
		if !p.IsInt64() {
			continue
		}
		// take the lowest priority as the tx priority
		if priority == 0 || p.Int64() < priority {
			priority = p.Int64()
		}
	}

	return priority
}
