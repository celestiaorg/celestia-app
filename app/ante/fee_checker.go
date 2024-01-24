package ante

import (
	"fmt"

	errors "cosmossdk.io/errors"
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

	fee := feeTx.GetFee().AmountOf(appconsts.BondDenom)
	gas := feeTx.GetGas()
	appVersion := ctx.BlockHeader().Version.App

	// global minimum fee only applies to app versions greater than one
	if appVersion > v1.Version {
		globalMinGasPrice, err := appconsts.GlobalMinGasPrice(appVersion)
		if err != nil {
			return nil, 0, errors.Wrapf(err, "failed to get GlobalMinGasPrice for app version %d", appVersion)
		}

		// convert the global minimum gas price to a big.Int
		globalMinGasPriceInt, err := sdk.NewDecFromStr(fmt.Sprintf("%f", globalMinGasPrice))
		if err != nil {
			return nil, 0, errors.Wrap(err, "invalid GlobalMinGasPrice")
		}

		gasInt := sdk.NewIntFromUint64(gas)
		minFee := globalMinGasPriceInt.MulInt(gasInt).RoundInt()

		if !fee.GTE(minFee) {
			return nil, 0, errors.Wrapf(sdkerror.ErrInsufficientFee, "insufficient fees; got: %s required: %s", fee, minFee)
		}
	}

	priority := getTxPriority(feeTx.GetFee(), int64(gas))
	return feeTx.GetFee(), priority, nil
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
