package types

import (
	"sigs.k8s.io/yaml"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// Parameter store keys
var (
	KeyMintDenom     = []byte("MintDenom")
	KeyBlocksPerYear = []byte("BlocksPerYear")
)

// Default # of blocks per year if not configured in genesis.
// Assuming 15 second block times
const DefaultBlocksPerYear = uint64(60 * 60 * 8766 / 15)

// ParamTable for minting module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func NewParams() Params {
	return Params{}
}

// default minting module parameters
func DefaultParams() Params {
	return Params{}
}

// Validate validates the params
func (p Params) Validate() error {
	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

// Implements params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{}
}
