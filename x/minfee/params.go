package minfee

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const ModuleName = "minfee"

var _ paramtypes.ParamSet = (*Params)(nil)

var (
	KeyNetworkMinGasPrice     = []byte("NetworkMinGasPrice")
	DefaultNetworkMinGasPrice math.LegacyDec
)

func init() {
	DefaultNetworkMinGasPriceDec, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", appconsts.DefaultNetworkMinGasPrice))
	if err != nil {
		panic(err)
	}
	DefaultNetworkMinGasPrice = DefaultNetworkMinGasPriceDec
}

type Params struct {
	NetworkMinGasPrice math.LegacyDec
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
		paramtypes.NewParamSetPair(KeyNetworkMinGasPrice, &p.NetworkMinGasPrice, ValidateMinGasPrice),
	}
}

// Validate validates the param type
func ValidateMinGasPrice(i interface{}) error {
	_, ok := i.(math.LegacyDec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}
