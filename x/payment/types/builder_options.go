package types

import (
	fmt "fmt"

	sdkclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

type TxBuilderOption func(builder sdkclient.TxBuilder) sdkclient.TxBuilder

func SetGasLimit(limit uint64) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetGasLimit(limit)
		return builder
	}
}

func SetFeeAmount(fees sdk.Coins) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeeAmount(fees)
		return builder
	}
}

func SetMemo(memo string) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetMemo(memo)
		return builder
	}
}

func SetFeePayer(feePayer sdk.AccAddress) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeePayer(feePayer)
		return builder
	}
}

func SetTip(tip *tx.Tip) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetTip(tip)
		return builder
	}
}

func SetTimeoutHeight(height uint64) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetTimeoutHeight(height)
		return builder
	}
}

func SetFeeGranter(feeGranter sdk.AccAddress) TxBuilderOption {
	return func(builder sdkclient.TxBuilder) sdkclient.TxBuilder {
		builder.SetFeeGranter(feeGranter)
		return builder
	}
}

// InheritTxConfig sets all of the accessible configurations from a given tx
// into a a give client.TxBuilder
func InheritTxConfig(builder sdkclient.TxBuilder, tx authsigning.Tx) sdkclient.TxBuilder {
	if gas := tx.GetGas(); gas != 0 {
		fmt.Println("setting ***********************************", gas)
		builder.SetGasLimit(gas)
	}

	if feeAmmount := tx.GetFee(); !feeAmmount.AmountOf("utia").Equal(sdk.NewInt(0)) {
		fmt.Println("setting ***********************************", feeAmmount.String())
		builder.SetFeeAmount(tx.GetFee())
	}

	if memo := tx.GetMemo(); memo != "" {
		fmt.Println("setting ***********************************", memo)
		builder.SetMemo(tx.GetMemo())
	}

	if tip := tx.GetTip(); tip != nil {
		fmt.Println("setting ***********************************", tip)
		builder.SetTip(tip)
	}

	if timeoutHeight := tx.GetTimeoutHeight(); timeoutHeight != 0 {
		fmt.Println("setting ***********************************", timeoutHeight)
		builder.SetTimeoutHeight(timeoutHeight)
	}

	if feePayer := tx.FeePayer(); !feePayer.Empty() {
		fmt.Println("setting *********************************** fee payour", feePayer.String())
		// builder.SetFeePayer(tx.FeePayer())
	}

	if feeGranter := tx.FeeGranter(); !feeGranter.Empty() {
		fmt.Println("setting ***********************************", feeGranter.String())
		builder.SetFeeGranter(tx.FeeGranter())
	}

	return builder
}
