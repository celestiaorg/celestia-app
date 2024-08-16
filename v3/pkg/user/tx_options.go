package user

import (
	"math"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
)

type TxOption func(builder sdkclient.TxBuilder) sdkclient.TxBuilder

func SetGasLimit(limit uint64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetGasLimit(limit)
		return builder
	}
}

func SetFee(fees uint64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(int64(fees)))))
		return builder
	}
}

func SetMemo(memo string) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetMemo(memo)
		return builder
	}
}

func SetFeePayer(feePayer sdk.AccAddress) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeePayer(feePayer)
		return builder
	}
}

func SetTip(tip *tx.Tip) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetTip(tip)
		return builder
	}
}

func SetTimeoutHeight(height uint64) TxOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetTimeoutHeight(height)
		return builder
	}
}

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
