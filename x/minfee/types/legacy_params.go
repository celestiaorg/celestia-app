package types

import (
	"fmt"

	"cosmossdk.io/math"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// TODO: this file can be removed once the upgrade to self managed modules has been completed.

var _ paramtypes.ParamSet = (*Params)(nil)

var KeyNetworkMinGasPrice = []byte("NetworkMinGasPrice")

func init() {
	DefaultNetworkMinGasPriceDec, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultNetworkMinGasPrice))
	if err != nil {
		panic(err)
	}
	DefaultNetworkMinGasPrice = DefaultNetworkMinGasPriceDec
}

// RegisterMinFeeParamTable returns a subspace with a key table attached.
func RegisterMinFeeParamTable(subspace paramtypes.Subspace) paramtypes.Subspace {
	if subspace.HasKeyTable() {
		return subspace
	}
	return subspace.WithKeyTable(ParamKeyTable())
}

// ParamKeyTable returns the param key table for the minfee module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// ParamSetPairs gets the param key-value pair
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyNetworkMinGasPrice, &p.NetworkMinGasPrice, validateMinGasPrice),
	}
}

// validateMinGasPrice validates the param type
func validateMinGasPrice(i interface{}) error {
	_, ok := i.(math.LegacyDec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}
