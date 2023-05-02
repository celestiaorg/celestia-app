package types

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	BlocksPerYear        = 6311520
	InitialInflationRate = 0.08
	DisinflationRate     = 0.1
	TargetInflationRate  = 0.015
)

var (
	initInflationRate   = sdk.NewDecWithPrec(InitialInflationRate*1000, 3)
	disinflationRate    = sdk.NewDecWithPrec(DisinflationRate*1000, 3)
	targetInflationRate = sdk.NewDecWithPrec(TargetInflationRate*1000, 3)
)
