package ante

import (
	"fmt"

	errors "cosmossdk.io/errors"
	// "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerror "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// priorityScalingFactor is a scaling factor to convert the gas price to a priority.
	priorityScalingFactor = 1_000_000
)

// CheckTxFeeWithGlobalMinGasPrices implements the default fee logic, where the minimum price per
// unit of gas is fixed and set globally, and the tx priority is computed from the gas price.
func CheckTxFeeWithGlobalMinGasPrices(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errors.Wrap(sdkerror.ErrTxDecode, "Tx must be a FeeTx")
	}

	// global minimum fee only applies to app versions greater than one
	if ctx.BlockHeader().Version.App > v1.Version {
		err := CheckTxFeeWithMinGasPrices(feeTx, appconsts.GlobalMinGasPrice, "insufficient global minimum fee")
		if err != nil {
			return nil, 0, err
		}
	}

	priority := getTxPriority(feeTx.GetFee(), int64(feeTx.GetGas()))
	return feeTx.GetFee(), priority, nil
}

// CheckTxFeeWithMinGasPrices validates that the provided transaction fee is sufficient given the provided minimum gas price.
// It now also extracts the fee and gas from the FeeTx and returns them.
func CheckTxFeeWithMinGasPrices(feeTx sdk.FeeTx, minGasPrice float64, errMsg string) error {
    fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
    gas := feeTx.GetGas()

    minGasPriceDec, err := sdk.NewDecFromStr(fmt.Sprintf("%f", minGasPrice))
    if err != nil {
        return errors.Wrap(err, "invalid minGasPrice: %s")
    }

    minFee := minGasPriceDec.MulInt(sdk.NewIntFromUint64(gas)).RoundInt()
    if !fee.GTE(minFee) {
        return errors.Wrapf(sdkerror.ErrInsufficientFee, "%s; got: %s required: %s", errMsg, fee, minFee)
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
