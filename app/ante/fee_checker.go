package ante

import (
	"fmt"

	errors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerror "github.com/cosmos/cosmos-sdk/types/errors"
	params "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/celestiaorg/celestia-app/x/minfee"
	"cosmossdk.io/math"
)

const (
	// priorityScalingFactor is a scaling factor to convert the gas price to a priority.
	priorityScalingFactor = 1_000_000
)

// CheckTxFeeWithGlobalMinGasPrices implements the default fee logic, where the minimum price per
// unit of gas is fixed and set globally, and the tx priority is computed from the gas price.
func CheckTxFeeWithGlobalMinGasPrices(ctx sdk.Context, tx sdk.Tx, minFeeParams params.Subspace) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errors.Wrap(sdkerror.ErrTxDecode, "Tx must be a FeeTx")
	}

	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
	gas := feeTx.GetGas()

	if ctx.IsCheckTx() {
	    defaultMinGasPrice := appconsts.DefaultMinGasPrice
		err := CheckTxFeeWithMinGasPrices(fee, gas, defaultMinGasPrice, "insufficient validator minimum fee")
		if err != nil {
			return nil, 0, err
		}
	}

	// global minimum fee only applies to app versions greater than one
	if ctx.BlockHeader().Version.App > v1.Version {
		var globalMinFee sdk.Dec
		minFeeParams.Get(ctx, minfee.KeyMinGasPrice, &globalMinFee)

		globalMinGasPrice := appconsts.GlobalMinGasPrice(ctx.BlockHeader().Version.App)
		err := CheckTxFeeWithMinGasPrices(fee, gas, globalMinGasPrice, "insufficient global minimum fee")
		if err != nil {
			return nil, 0, err
		}
	}

	priority := getTxPriority(feeTx.GetFee(), int64(feeTx.GetGas()))
	return feeTx.GetFee(), priority, nil
}

// CheckTxFeeWithMinGasPrices validates that the provided transaction fee is sufficient given the provided minimum gas price.
func CheckTxFeeWithMinGasPrices(fee math.Int, gas uint64, minGasPrice float64, errMsg string) error {
	minGasPriceDec, err := sdk.NewDecFromStr(fmt.Sprintf("%f", minGasPrice))
	if err != nil {
		return errors.Wrapf(err, "invalid minGasPrice: %f", minGasPriceDec)
	}

	minFee := minGasPriceDec.MulInt(sdk.NewIntFromUint64(gas)).RoundInt()
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
