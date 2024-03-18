package ante

import (
	"fmt"

	errors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	minfee "github.com/celestiaorg/celestia-app/x/minfee"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerror "github.com/cosmos/cosmos-sdk/types/errors"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

const (
	// priorityScalingFactor is a scaling factor to convert the gas price to a priority.
	priorityScalingFactor = 1_000_000
)

// CheckTxFeeWithGlobalMinGasPrices implements the default fee logic, where the minimum price per
// unit of gas is fixed and set globally, and the tx priority is computed from the gas price.
func CheckTxFeeWithGlobalMinGasPrices(ctx sdk.Context, tx sdk.Tx, paramKeeper params.Keeper) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errors.Wrap(sdkerror.ErrTxDecode, "Tx must be a FeeTx")
	}

	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
	gas := feeTx.GetGas()

	// Ensure that the provided fee meets a minimum threshold for the validator.
	// This is only for local mempool purposes, and thus
	// is only ran on check tx.
	if ctx.IsCheckTx() {
		defaultMinGasPriceDec, err := sdk.NewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultMinGasPrice))
		if err != nil {
			return nil, 0, errors.Wrapf(err, "invalid defaultMinGasPrice: %f", defaultMinGasPriceDec)
		}

		err = CheckTxFeeWithMinGasPrices(fee, gas, defaultMinGasPriceDec, "insufficient validator minimum fee")
		if err != nil {
			return nil, 0, err
		}
	}

	// global minimum fee only applies to app versions greater than one
	if ctx.BlockHeader().Version.App > v1.Version {
		// fmt.Println("GOT HEREEEEEEE")
		subspace, exists := paramKeeper.GetSubspace(minfee.ModuleName)
		if !exists {
			return nil, 0, errors.Wrap(sdkerror.ErrInvalidRequest, "minfee is not a registered subspace")
		}

		if !subspace.Has(ctx, minfee.KeyGlobalMinGasPrice) {
			return nil, 0, errors.Wrap(sdkerror.ErrKeyNotFound, "GlobalMinGasPrice")
		}

		var globalMinGasPrice sdk.Dec
		// gets the global minimum gas price from the param store
		// panics if not configured properly
		subspace.Get(ctx, minfee.KeyGlobalMinGasPrice, &globalMinGasPrice)

		err := CheckTxFeeWithMinGasPrices(fee, gas, globalMinGasPrice, "insufficient global minimum fee")
		if err != nil {
			return nil, 0, err
		}
	}

	priority := getTxPriority(feeTx.GetFee(), int64(gas))
	return feeTx.GetFee(), priority, nil
}

// CheckTxFeeWithMinGasPrices validates that the provided transaction fee is sufficient given the provided minimum gas price.
func CheckTxFeeWithMinGasPrices(fee math.Int, gas uint64, minGasPrice sdk.Dec, errMsg string) error {
	minFee := minGasPrice.MulInt(sdk.NewIntFromUint64(gas)).RoundInt()
	if fee.LT(minFee) {
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
		p := c.Amount.Mul(sdk.NewInt(priorityScalingFactor)).QuoRaw(gas)
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
