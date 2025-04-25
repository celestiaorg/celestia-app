package ante

import (
	"cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerror "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	minfeekeeper "github.com/celestiaorg/celestia-app/v4/x/minfee/keeper"
)

const (
	// priorityScalingFactor is a scaling factor to convert the gas price to a priority.
	priorityScalingFactor = 1_000_000
)

// ValidateTxFeeWrapper enables the passing of an additional minfeeKeeper parameter in
// ante.NewDeductFeeDecorator whilst still satisfying the ante.TxFeeChecker type.
func ValidateTxFeeWrapper(minfeeKeeper *minfeekeeper.Keeper) ante.TxFeeChecker {
	return func(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
		return ValidateTxFee(ctx, tx, minfeeKeeper)
	}
}

// ValidateTxFee implements default fee validation logic for transactions.
// It ensures that the provided transaction fee meets a minimum threshold for the node
// as well as a network minimum threshold and computes the tx priority based on the gas price.
func ValidateTxFee(ctx sdk.Context, tx sdk.Tx, minfeeKeeper *minfeekeeper.Keeper) (sdk.Coins, int64, error) {
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

	networkMinGasPrice := minfeeKeeper.GetParams(ctx).NetworkMinGasPrice

	err := verifyMinFee(fee, gas, networkMinGasPrice, "insufficient gas price for the network")
	if err != nil {
		return nil, 0, err
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
		providedGasPrice := math.LegacyNewDecFromInt(fee).QuoInt64(int64(gas))
		return errors.Wrapf(sdkerror.ErrInsufficientFee, "%s; got fee: %s and gas price of %s but required at least: %s and a minimum gas price of %s", errMsg, fee, providedGasPrice, minFee, minGasPrice)
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
