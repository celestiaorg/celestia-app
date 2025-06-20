package user

import (
	"math"

	sdkmath "cosmossdk.io/math"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// TxOption is a function that configures a transaction builder.
type TxOption func(builder sdkclient.TxBuilder) sdkclient.TxBuilder

// SetGasLimit sets the gas limit for a transaction.
func SetGasLimit(limit uint64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetGasLimit(limit)
		return builder
	}
}

// SetFee sets the fee amount for a transaction.
func SetFee(fees uint64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(int64(fees)))))
		return builder
	}
}

// SetMemo sets the memo for a transaction.
func SetMemo(memo string) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetMemo(memo)
		return builder
	}
}

// SetFeePayer sets the fee payer address for a transaction.
func SetFeePayer(feePayer sdk.AccAddress) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeePayer(feePayer)
		return builder
	}
}

// SetTimeoutHeight sets the timeout height for a transaction.
func SetTimeoutHeight(height uint64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetTimeoutHeight(height)
		return builder
	}
}

// SetFeeGranter sets the fee granter address for a transaction.
func SetFeeGranter(feeGranter sdk.AccAddress) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeeGranter(feeGranter)
		return builder
	}
}

// SetGasLimitAndGasPrice sets the gas limit and fee using the provided gas price and
// gas limit. Note that this could overwrite or be overwritten by other
// conflicting TxOptions.
func SetGasLimitAndGasPrice(gasLimit uint64, gasPrice float64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetGasLimit(gasLimit)
		builder.SetFeeAmount(
			sdk.NewCoins(
				sdk.NewInt64Coin(appconsts.BondDenom, int64(math.Ceil(gasPrice*float64(gasLimit)))),
			),
		)
		return builder
	}
}
