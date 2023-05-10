package types

import sdk "github.com/cosmos/cosmos-sdk/types"

const (
	SecondsPerMinute = 60
	MinutesPerHour   = 60
	HoursPerDay      = 24
	DaysPerYear      = 365
	SecondsPerYear   = SecondsPerMinute * MinutesPerHour * HoursPerDay * DaysPerYear // 31,536,000

	// BlocksPerYear is an estimate for the number of blocks produced by the
	// Celestia blockchain per year. This number is based on the assumption that
	// the block interval (i.e. TargetHeightDuration) is 15 seconds.
	//
	// 31,536,000 seconds in a year / 15 seconds per block = 2,102,400 blocks per year.
	BlocksPerYear        = 2_102_400
	InitialInflationRate = 0.08
	DisinflationRate     = 0.1
	TargetInflationRate  = 0.015
)

var (
	blocksPerYear       = sdk.NewInt(int64(BlocksPerYear))
	initalInflationRate = sdk.NewDecWithPrec(InitialInflationRate*1000, 3)
	disinflationRate    = sdk.NewDecWithPrec(DisinflationRate*1000, 3)
	targetInflationRate = sdk.NewDecWithPrec(TargetInflationRate*1000, 3)
)
